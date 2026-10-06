package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/types"
)

// ── Project rules ─────────────────────────────────────────────────────────────

// RuleFiles are the instruction files a project keeps for coding agents,
// read from the workspace root in this order.
var RuleFiles = []string{"AGENTS.md", "CLAUDE.md", ".cursorrules", ".github/copilot-instructions.md", ".rove/rules.md"}

// rulesBudget caps what the rules add to every request (characters).
const rulesBudget = 8000

type rulesEntry struct {
	stamp string
	text  string
	files []string
}

var rulesCache sync.Map // workspace → rulesEntry

// ProjectRules returns the workspace's rule files as a system prompt block
// (empty when it has none) and the files it read. It is re-read only when a
// file changes.
func ProjectRules(workspace string) (string, []string) {
	if workspace == "" {
		return "", nil
	}
	var stamp strings.Builder
	var found []string
	for _, name := range RuleFiles {
		if st, err := os.Stat(filepath.Join(workspace, name)); err == nil && !st.IsDir() {
			fmt.Fprintf(&stamp, "%s:%d:%d;", name, st.Size(), st.ModTime().UnixNano())
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return "", nil
	}
	if e, ok := rulesCache.Load(workspace); ok && e.(rulesEntry).stamp == stamp.String() {
		return e.(rulesEntry).text, e.(rulesEntry).files
	}
	var b strings.Builder
	b.WriteString("\n\n## Project rules\nThe project's own instructions for coding agents. Follow them.\n")
	left := rulesBudget
	var used []string
	for _, name := range found {
		if left <= 0 {
			break
		}
		data, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			continue
		}
		if len(text) > left {
			text = text[:left] + "\n…(cut)"
		}
		left -= len(text)
		fmt.Fprintf(&b, "\n### %s\n%s\n", name, text)
		used = append(used, name)
	}
	out := ""
	if len(used) > 0 {
		out = b.String()
	}
	rulesCache.Store(workspace, rulesEntry{stamp: stamp.String(), text: out, files: used})
	return out, used
}

// ── Turn edits ────────────────────────────────────────────────────────────────

// maxEditSize: files larger than this are not kept for review.
const maxEditSize = 1 << 20

// captureEdit keeps what a file held before this run first writes it. It
// reports whether the file was newly recorded for this run.
func (rt *Runtime) captureEdit(ctx context.Context, runID types.ID, req RunRequest, argsJSON string) bool {
	if req.Workspace == "" || req.SessionID == "" || rt.store == nil {
		return false
	}
	p := extractPathArg(argsJSON)
	if p == "" {
		return false
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(req.Workspace, p)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(req.Workspace, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	e := types.TurnEdit{RunID: runID, SessionID: req.SessionID, Workspace: req.Workspace, Path: filepath.ToSlash(rel)}
	if st, err := os.Stat(abs); err == nil {
		if st.IsDir() || st.Size() > maxEditSize {
			return false
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return false
		}
		e.Before, e.Existed = string(data), true
	}
	added, err := rt.store.PutTurnEdit(ctx, e)
	return err == nil && added
}

// ── Compaction ────────────────────────────────────────────────────────────────

const (
	defaultKeepTurns = 4
	// what the summarizer reads, at most (characters)
	compactInput  = 60000
	compactPerMsg = 1500
)

const compactPrompt = `You compact a coding chat so it can continue with less context. Write a summary of the conversation below that a coding agent can pick up from: the user's goals and preferences, decisions made, files created or changed and their state, commands run and their results, open problems and next steps. Keep names, paths and numbers exact. Be concise: at most about 400 words, in the conversation's language. Reply with the summary only.`

// estimateTokens is a rough count (4 characters a token) of what messages
// cost to send.
func estimateTokens(msgs []types.Message) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
		for _, c := range m.ToolCalls {
			n += len(c.Name) + len(c.ArgsJSON)
		}
	}
	return n / 4
}

// sinceSummary splits a chat at its last summary: the summary's text and
// the messages after it.
func sinceSummary(hist []types.Message) (string, []types.Message) {
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].Kind == types.MessageSummary {
			return hist[i].Content, hist[i+1:]
		}
	}
	return "", hist
}

// Compact summarizes a chat's older messages now (/compact).
func (rt *Runtime) Compact(ctx context.Context, sessionID, agentID types.ID) (bool, error) {
	ag, err := rt.store.GetAgent(ctx, agentID)
	if err != nil {
		return false, err
	}
	var ps Persona
	if rt.persona != nil {
		ps = rt.persona(ctx, sessionID)
	}
	prov, model := ag.Provider, ag.Model
	if ps.Model != "" {
		model = ps.Model
		if ps.Provider != "" {
			prov = ps.Provider
		}
	}
	completer, model, err := rt.router.Resolve(ag.Profile, prov, model)
	if err != nil {
		return false, err
	}
	return rt.maybeCompact(ctx, completer, model, sessionID, true)
}

// maybeCompact summarizes the part of a chat before its last KeepTurns user
// turns once the chat (since its last summary) passes CompactAt tokens, or
// always when forced. The messages stay in the chat for the user; the model
// is sent the summary instead of them from then on.
func (rt *Runtime) maybeCompact(ctx context.Context, completer provider.Completer, model string, sessionID types.ID, force bool) (bool, error) {
	if rt.sessions == nil || (!force && rt.CompactAt <= 0) {
		return false, nil
	}
	hist, err := rt.sessions.History(ctx, sessionID)
	if err != nil {
		return false, err
	}
	prev, tail := sinceSummary(hist)
	if !force && estimateTokens(tail) < rt.CompactAt {
		return false, nil
	}
	keep := rt.KeepTurns
	if keep <= 0 {
		keep = defaultKeepTurns
	}
	if force {
		keep = 1
	}
	// cut at a user turn, so a tool call and its result stay together
	cut, seen := -1, 0
	for i := len(tail) - 1; i >= 0; i-- {
		if tail[i].Role == types.RoleUser && tail[i].Kind == "" {
			seen++
			if seen == keep {
				cut = i
				break
			}
		}
	}
	if cut <= 0 {
		return false, nil
	}
	old := tail[:cut]

	var b strings.Builder
	if prev != "" {
		b.WriteString("Summary of what came before:\n" + prev + "\n\n")
	}
	var lines []string
	for _, m := range old {
		line := string(m.Role) + ": " + clipText(m.Content, compactPerMsg)
		for _, c := range m.ToolCalls {
			line += "\n  → " + c.Name + " " + clipText(c.ArgsJSON, 200)
		}
		if len(m.Images) > 0 {
			line += fmt.Sprintf("\n  [%d image(s)]", len(m.Images))
		}
		lines = append(lines, line)
	}
	text := strings.Join(lines, "\n")
	if len(text) > compactInput {
		text = "…\n" + text[len(text)-compactInput:]
	}
	b.WriteString(text)

	compactReq := provider.ChatRequest{Model: model, Stream: true, Messages: []provider.ChatMessage{
		{Role: "system", Content: compactPrompt},
		{Role: "user", Content: b.String()},
	}}
	ch, err := completer.Complete(ctx, compactReq)
	if err != nil {
		return false, err
	}
	call := rt.meter("compact", sessionID, completer, model, compactReq)
	var out strings.Builder
	for d := range ch {
		call.see(d)
		out.WriteString(d.Content)
	}
	call.done()
	summary := strings.TrimSpace(out.String())
	if summary == "" || ctx.Err() != nil {
		return false, ctx.Err()
	}
	// the summary sits right before the first message it does not cover
	_, err = rt.sessions.Append(ctx, types.Message{
		SessionID: sessionID,
		Role:      types.RoleSystem,
		Kind:      types.MessageSummary,
		Content:   summary,
		CreatedAt: tail[cut].CreatedAt.Add(-time.Nanosecond),
	})
	if err == nil && rt.bus != nil {
		rt.bus.Publish(types.Event{Type: "session.compacted", Topic: "session." + string(sessionID), Payload: map[string]any{"sessionId": string(sessionID), "messages": len(old)}})
	}
	return err == nil, err
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// imageTurns: only the last few user turns send their images again; older
// ones are named, not re-sent (images are the most expensive input).
const imageTurns = 2
