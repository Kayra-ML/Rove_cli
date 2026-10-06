package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/checkpoint"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/memory"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

type Runtime struct {
	store    *store.Store
	bus      *eventbus.Bus
	sessions *session.Manager
	memory   *memory.System
	router   *provider.Router
	tools    *tool.Runtime
	checkpt  *checkpoint.Manager
	onEdit   func(workspace string, paths []string)
	onDone   func(req RunRequest, res RunResult)
	persona  func(ctx context.Context, sessionID types.ID) Persona
	// CompactAt: when a chat's history (since its last summary) passes this
	// many estimated tokens, the older part is summarized before the next
	// model call. KeepTurns user turns stay word for word. 0 turns it off.
	CompactAt int
	KeepTurns int
	// claims, when set, guards file edits between sessions (terminals).
	claims  Claims
	mu      sync.Mutex
	seq     uint64
	cancels map[string]runHandle
	// steers are words for a run already under way, waiting for its next
	// turn; see Steer.
	steers map[types.ID][]string
}

// runHandle is one in-flight run. Runs are keyed by session, so two sessions
// sharing an agent profile do not cancel each other.
type runHandle struct {
	agent  types.ID
	seq    uint64
	cancel context.CancelFunc
}

func New(s *store.Store, bus *eventbus.Bus, sess *session.Manager, mem *memory.System, r *provider.Router, t *tool.Runtime) *Runtime {
	return &Runtime{store: s, bus: bus, sessions: sess, memory: mem, router: r, tools: t, cancels: map[string]runHandle{}}
}

// SetCheckpointManager wires in the checkpoint manager for auto-snapshotting
// before mutating tool calls (file writes, git commits, shell exec).
func (rt *Runtime) SetCheckpointManager(c *checkpoint.Manager) {
	rt.checkpt = c
}

// SetEditHook is called after a tool writes files, with the workspace root
// and the paths the tool touched. The project map uses it to reindex.
func (rt *Runtime) SetEditHook(fn func(workspace string, paths []string)) {
	rt.onEdit = fn
}

// Persona is who the agent is inside one session: a system prompt that
// replaces the generic one, a tool filter, and an optional model override.
type Persona struct {
	Prompt   string
	Allow    func(tool string) bool
	Model    string
	Provider string
	// Effort is the chat's reasoning effort ("low", "medium", "high");
	// empty leaves it to the model.
	Effort string
	// Context is changing context (what other terminals are doing) added at
	// the very end of the system prompt, after everything cacheable.
	Context string
}

// Claims keeps two runs from editing the same file at once. Claim asks to
// write path for sessionID: it returns "" when allowed, else who holds it.
// Release frees a session's files when its run ends.
type Claims interface {
	Claim(ctx context.Context, sessionID types.ID, workspace, path string) (holder string)
	Release(sessionID types.ID)
}

// SetClaims installs the file-edit guard.
func (rt *Runtime) SetClaims(c Claims) { rt.claims = c }

// Running reports whether a session has a run in progress.
func (rt *Runtime) Running(sessionID types.ID) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	_, ok := rt.cancels["s:"+string(sessionID)]
	return ok
}

// SetPersonaSource installs the per-session persona lookup.
func (rt *Runtime) SetPersonaSource(fn func(ctx context.Context, sessionID types.ID) Persona) {
	rt.persona = fn
}

func (p Persona) allows(name string) bool { return p.Allow == nil || p.Allow(name) }

// Allows reports whether the persona offers a tool to the model.
func (p Persona) Allows(name string) bool { return p.allows(name) }

// PersonaFor is the persona a run in this session would get.
func (rt *Runtime) PersonaFor(ctx context.Context, sessionID types.ID) Persona {
	if rt.persona == nil || sessionID == "" {
		return Persona{}
	}
	return rt.persona(ctx, sessionID)
}

// SetRunHook is called (in its own goroutine) after a run finishes without
// error. The context map uses it to tell linked sessions what changed.
func (rt *Runtime) SetRunHook(fn func(req RunRequest, res RunResult)) {
	rt.onDone = fn
}

func (rt *Runtime) Upsert(ctx context.Context, a types.Agent) (types.Agent, error) {
	now := time.Now().UTC()
	if a.ID == "" {
		a.ID = id.NewID()
		a.CreatedAt = now
	}
	if a.Status == "" {
		a.Status = types.AgentIdle
	}
	a.UpdatedAt = now
	return a, rt.store.UpsertAgent(ctx, a)
}

func (rt *Runtime) Get(ctx context.Context, id types.ID) (types.Agent, error) {
	return rt.store.GetAgent(ctx, id)
}

func (rt *Runtime) List(ctx context.Context) ([]types.Agent, error) {
	return rt.store.ListAgents(ctx)
}

type RunRequest struct {
	AgentID     types.ID
	SessionID   types.ID
	WorkspaceID types.ID
	Workspace   string
	UserMessage string
	SystemExtra string
	MaxTurns    int
	// Relay metadata: sessions this run's trigger already passed through,
	// and how many links it has crossed. Empty for runs a human started.
	Chain []types.ID
	Hop   int
	// HistoryLimit, when > 0, sends only the last N history messages (cut at
	// a user turn). Relay runs use it so a cable does not replay a whole
	// transcript into the model.
	HistoryLimit int
	// HistoryTurns, when > 0, sends only the last N user turns and what
	// followed each (the run's own tool turns always stay whole). Goal runs
	// use it so iterations do not replay the whole chat.
	HistoryTurns int
	// AllowedTools, when non-nil, narrows the tools offered to (and callable
	// by) this run, on top of the session's persona. Goal runs use it for
	// their harness tool policy.
	AllowedTools []string
	// ToolConcurrency > 1 runs a turn's read-only tool calls (see
	// readOnlyTools) side by side, at most this many at once; their results
	// are still recorded in call order.
	ToolConcurrency int
	// Model and Provider, when set, override every other model choice for
	// this run (a goal's model fallback).
	Model    string
	Provider string
	// Images (data: URLs) go with UserMessage to vision models.
	Images []string
}

// readOnlyTools are the built-in tools safe to run side by side: they never
// change the workspace.
var readOnlyTools = map[string]bool{"read_file": true, "list_dir": true, "git_status": true}

type RunResult struct {
	// RunID names this run; the files it changed are kept under it for
	// review (edits.list).
	RunID       types.ID
	Assistant   string
	Turns       int
	Done        bool
	ToolsCalled []string
	FilesEdited []string
}

// Cancel stops every run of an agent, in any session.
func (rt *Runtime) Cancel(agentID types.ID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for k, h := range rt.cancels {
		if h.agent == agentID {
			h.cancel()
			delete(rt.cancels, k)
		}
	}
}

// CancelSession stops the run in one session.
func (rt *Runtime) CancelSession(sessionID types.ID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if h, ok := rt.cancels["s:"+string(sessionID)]; ok {
		h.cancel()
		delete(rt.cancels, "s:"+string(sessionID))
	}
}

// Steer hands a running session a message it reads at its next turn,
// without stopping it: a subagent that has gone the wrong way can be turned
// rather than killed and started over. It reports false when nothing is
// running there, so the caller can say so instead of the words vanishing.
func (rt *Runtime) Steer(sessionID types.ID, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || !rt.Busy(sessionID) {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.steers == nil {
		rt.steers = map[types.ID][]string{}
	}
	rt.steers[sessionID] = append(rt.steers[sessionID], text)
	return true
}

// takeSteers empties a session's waiting steers.
func (rt *Runtime) takeSteers(sessionID types.ID) []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := rt.steers[sessionID]
	delete(rt.steers, sessionID)
	return out
}

// Busy reports whether a session has a run in flight.
func (rt *Runtime) Busy(sessionID types.ID) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	_, ok := rt.cancels["s:"+string(sessionID)]
	return ok
}

func runKey(req RunRequest) string {
	if req.SessionID != "" {
		return "s:" + string(req.SessionID)
	}
	return "a:" + string(req.AgentID)
}

func (rt *Runtime) Run(ctx context.Context, req RunRequest) (res RunResult, err error) {
	ctx, cancel := context.WithCancel(ctx)
	runID := id.NewID()
	edited := 0 // files whose earlier content this run kept for review
	key := runKey(req)
	rt.mu.Lock()
	if prev, ok := rt.cancels[key]; ok {
		prev.cancel()
	}
	rt.seq++
	h := runHandle{agent: req.AgentID, seq: rt.seq, cancel: cancel}
	rt.cancels[key] = h
	rt.mu.Unlock()
	defer func() {
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("run %s (session %s): %v", req.AgentID, req.SessionID, err)
		}
	}()
	defer func() {
		// A distinct signal from session.Manager's per-append "message.done":
		// this fires exactly once per run, even on an early failure that
		// never appends anything (e.g. an unresolvable model), so a job
		// status watching this session from outside — the sidebar's dot,
		// another pane, a relay — always learns the run ended one way or
		// another.
		if rt.bus != nil && req.SessionID != "" {
			payload := map[string]any{"sessionId": string(req.SessionID), "runId": string(runID), "edits": edited}
			if err != nil {
				payload["error"] = err.Error()
			}
			rt.bus.Publish(types.Event{Type: types.EventRunDone, Topic: "session." + string(req.SessionID), Payload: payload})
		}
		cancel()
		rt.mu.Lock()
		// a newer run in the same session may have replaced us
		if cur, ok := rt.cancels[key]; ok && cur.seq == h.seq {
			delete(rt.cancels, key)
		}
		rt.mu.Unlock()
		if err == nil && rt.onDone != nil {
			go rt.onDone(req, res)
		}
	}()
	defer func() { res.RunID = runID }()
	if rt.claims != nil && req.SessionID != "" {
		defer rt.claims.Release(req.SessionID)
	}
	if req.SessionID != "" {
		defer rt.takeSteers(req.SessionID)
	}

	agent, err := rt.store.GetAgent(ctx, req.AgentID)
	if err != nil {
		return RunResult{}, err
	}
	agent.Status = types.AgentRunning
	agent.UpdatedAt = time.Now().UTC()
	_ = rt.store.UpsertAgent(ctx, agent)
	rt.emitStatus(agent)

	if req.UserMessage != "" && rt.sessions != nil && req.SessionID != "" {
		_, _ = rt.sessions.Append(ctx, types.Message{SessionID: req.SessionID, Role: types.RoleUser, Content: req.UserMessage, Images: req.Images})
	}

	var ps Persona
	if rt.persona != nil && req.SessionID != "" {
		ps = rt.persona(ctx, req.SessionID)
	}
	provName, modelName := agent.Provider, agent.Model
	if ps.Model != "" {
		modelName = ps.Model
		if ps.Provider != "" {
			provName = ps.Provider
		}
	}
	if req.Model != "" {
		modelName = req.Model
		if req.Provider != "" {
			provName = req.Provider
		}
	}
	// the run's own tool policy narrows the persona's
	if req.AllowedTools != nil {
		allowed := make(map[string]bool, len(req.AllowedTools))
		for _, n := range req.AllowedTools {
			allowed[n] = true
		}
		inner := ps.Allow
		ps.Allow = func(name string) bool { return allowed[name] && (inner == nil || inner(name)) }
	}
	completer, model, err := rt.router.Resolve(agent.Profile, provName, modelName)
	if err != nil {
		agent.Status = types.AgentFailed
		_ = rt.store.UpsertAgent(ctx, agent)
		rt.emitStatus(agent)
		return RunResult{}, err
	}
	// a long chat is summarized before it is sent again
	if req.SessionID != "" && req.HistoryTurns == 0 && req.HistoryLimit == 0 {
		rt.maybeCompact(ctx, completer, model, req.SessionID, false)
	}

	maxTurns := req.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 12
	}

	var last string
	var allToolsCalled []string
	failed := map[string]string{} // tool call → what it failed with, this run
	var allFilesEdited []string
	for turn := 0; turn < maxTurns; turn++ {
		// words sent to this run while it worked go in before its next look
		// at the chat, as the latest thing it was told
		if turn > 0 && req.SessionID != "" && rt.sessions != nil {
			for _, s := range rt.takeSteers(req.SessionID) {
				_, _ = rt.sessions.Append(ctx, types.Message{SessionID: req.SessionID, Role: types.RoleUser, Content: "[steer] " + s})
			}
		}
		msgs, err := rt.buildMessages(ctx, agent, req, ps)
		if err != nil {
			return RunResult{}, err
		}
		chatReq := provider.ChatRequest{Model: model, Messages: msgs, Stream: true, ReasoningEffort: ps.Effort, CacheKey: string(req.SessionID), Workdir: req.Workspace}
		if rt.tools != nil {
			for _, spec := range rt.tools.Specs() {
				if ps.allows(spec.Name) {
					chatReq.Tools = append(chatReq.Tools, spec)
				}
			}
		}
		ch, err := completer.Complete(ctx, chatReq)
		if err != nil {
			agent.Status = types.AgentFailed
			_ = rt.store.UpsertAgent(ctx, agent)
			rt.emitStatus(agent)
			return RunResult{}, err
		}
		var content strings.Builder
		var toolCalls []types.ToolCall
		call := rt.meter("chat", req.SessionID, completer, model, chatReq)
		for d := range ch {
			call.see(d)
			if d.Content != "" {
				content.WriteString(d.Content)
				if rt.bus != nil {
					rt.bus.Publish(types.Event{
						Type:  types.EventMessageDelta,
						Topic: "session." + string(req.SessionID),
						Payload: map[string]any{
							"sessionId": string(req.SessionID),
							"delta":     d.Content,
						},
					})
				}
			}
			if len(d.ToolCalls) > 0 {
				toolCalls = append(toolCalls, d.ToolCalls...)
			}
		}
		call.done()
		text := content.String()
		last = text
		if rt.sessions != nil && req.SessionID != "" {
			_, _ = rt.sessions.Append(ctx, types.Message{
				SessionID: req.SessionID,
				Role:      types.RoleAssistant,
				Content:   text,
				ToolCalls: toolCalls,
			})
		}
		if len(toolCalls) == 0 {
			agent.Status = types.AgentIdle
			_ = rt.store.UpsertAgent(ctx, agent)
			rt.emitStatus(agent)
			return RunResult{Assistant: last, Turns: turn + 1, Done: true, ToolsCalled: allToolsCalled, FilesEdited: allFilesEdited}, nil
		}
		// read-only calls may run side by side; everything is still
		// recorded below in call order
		pre := rt.runReadOnly(ctx, req, ps, toolCalls)
		for i, tc := range toolCalls {
			allToolsCalled = append(allToolsCalled, tc.Name)
			if rt.bus != nil {
				rt.bus.Publish(types.Event{
					Type:  types.EventToolStart,
					Topic: "session." + string(req.SessionID),
					Payload: map[string]any{
						"name":      tc.Name,
						"args":      json.RawMessage(tc.ArgsJSON),
						"sessionId": string(req.SessionID),
					},
				})
			}
			// keep what a file held before this run first changed it, so the
			// turn's changes can be reviewed and taken back
			if isFileEditTool(tc.Name) && ps.allows(tc.Name) && rt.captureEdit(ctx, runID, req, tc.ArgsJSON) {
				edited++
			}
			// Auto-checkpoint before any tool call that mutates the workspace.
			if req.Workspace != "" && rt.checkpt != nil && isMutatingTool(tc.Name) {
				_, _ = rt.checkpt.Take(req.Workspace, "auto:"+tc.Name)
			}
			var res tool.Result
			var callErr error
			// another session (terminal) is editing this file right now
			held := ""
			if rt.claims != nil && isFileEditTool(tc.Name) && req.SessionID != "" {
				if p := extractPathArg(tc.ArgsJSON); p != "" {
					held = rt.claims.Claim(ctx, req.SessionID, req.Workspace, p)
				}
			}
			if held != "" {
				res = tool.Result{Content: "not written: " + held + " is editing this file right now. Work on another file, or ask it with terminal_send, then try again.", IsError: true}
				callErr = fmt.Errorf("file held by %s", held)
			} else if r, ok := pre[i]; ok {
				res, callErr = r.res, r.err
			} else if ps.allows(tc.Name) {
				res, callErr = rt.tools.Call(ctx, tc.Name, tool.Context{
					AgentID: req.AgentID, WorkspaceID: req.WorkspaceID, Workspace: req.Workspace,
					SessionID: req.SessionID,
				}, json.RawMessage(tc.ArgsJSON))
			} else {
				res = tool.Result{Content: "tool " + tc.Name + " is turned off for this session", IsError: true, Kind: types.ToolRefused}
				callErr = fmt.Errorf("tool %s disabled", tc.Name)
			}
			out := res.Content
			if callErr != nil && out == "" {
				out = callErr.Error()
			}
			out = repeatNote(failed, tc.Name+"\x00"+tc.ArgsJSON, out, res.IsError || callErr != nil)
			// Track file writes/edits from write/patch/create tool calls.
			if isFileEditTool(tc.Name) && tc.ArgsJSON != "" {
				if p := extractPathArg(tc.ArgsJSON); p != "" {
					allFilesEdited = append(allFilesEdited, p)
					if rt.onEdit != nil && req.Workspace != "" && callErr == nil {
						rt.onEdit(req.Workspace, []string{p})
					}
				}
			}
			if rt.sessions != nil && req.SessionID != "" {
				tr := &types.ToolResult{ToolCallID: tc.ID, Name: tc.Name, Content: out, IsError: res.IsError || callErr != nil, Kind: res.Kind}
				_, _ = rt.sessions.Append(ctx, types.Message{SessionID: req.SessionID, Role: types.RoleTool, Content: out, ToolResult: tr})
			}
			if rt.bus != nil {
				rt.bus.Publish(types.Event{
					Type:  types.EventToolResult,
					Topic: "session." + string(req.SessionID),
					Payload: map[string]any{
						"name":      tc.Name,
						"content":   out,
						"isError":   res.IsError || callErr != nil,
						"kind":      res.Kind,
						"sessionId": string(req.SessionID),
					},
				})
			}
		}
	}
	agent.Status = types.AgentIdle
	_ = rt.store.UpsertAgent(ctx, agent)
	rt.emitStatus(agent)
	return RunResult{Assistant: last, Turns: maxTurns, Done: false, ToolsCalled: allToolsCalled, FilesEdited: allFilesEdited}, nil
}

type toolOutcome struct {
	res tool.Result
	err error
}

// runReadOnly runs a turn's allowed read-only tool calls concurrently when
// the request asks for it (ToolConcurrency > 1) and there is more than one;
// the caller uses the returned outcomes (by call index) instead of calling
// those tools again.
func (rt *Runtime) runReadOnly(ctx context.Context, req RunRequest, ps Persona, calls []types.ToolCall) map[int]toolOutcome {
	if req.ToolConcurrency <= 1 || rt.tools == nil {
		return nil
	}
	var idx []int
	for i, tc := range calls {
		if readOnlyTools[tc.Name] && ps.allows(tc.Name) {
			idx = append(idx, i)
		}
	}
	if len(idx) < 2 {
		return nil
	}
	out := make(map[int]toolOutcome, len(idx))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, req.ToolConcurrency)
	for _, i := range idx {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			tc := calls[i]
			res, err := rt.tools.Call(ctx, tc.Name, tool.Context{
				AgentID: req.AgentID, WorkspaceID: req.WorkspaceID, Workspace: req.Workspace,
				SessionID: req.SessionID,
			}, json.RawMessage(tc.ArgsJSON))
			mu.Lock()
			out[i] = toolOutcome{res, err}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	return out
}

// Ask sends one request without tools — a system prompt and a user message
// — to the agent's model and returns the whole reply. It touches no session
// and publishes no events; usage is metered like any run. Planners use it.
func (rt *Runtime) Ask(ctx context.Context, agentID types.ID, system, user string) (string, error) {
	reply, _, err := rt.AskModel(ctx, agentID, types.ModelRef{}, system, user)
	return reply, err
}

// AskModel is Ask on a chosen model (m.Model; m.Provider, if set, picks its
// provider); an empty m asks the agent's own model. It also says which model
// answered.
func (rt *Runtime) AskModel(ctx context.Context, agentID types.ID, m types.ModelRef, system, user string) (string, types.ModelRef, error) {
	ag, err := rt.store.GetAgent(ctx, agentID)
	if err != nil {
		return "", types.ModelRef{}, err
	}
	used := types.ModelRef{Provider: ag.Provider, Model: ag.Model}
	if m.Model != "" {
		used.Model = m.Model
		if m.Provider != "" {
			used.Provider = m.Provider
		}
	}
	completer, model, err := rt.router.Resolve(ag.Profile, used.Provider, used.Model)
	if err != nil {
		return "", used, err
	}
	askReq := provider.ChatRequest{Model: model, Stream: true, Ask: true, Messages: []provider.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}}
	ch, err := completer.Complete(ctx, askReq)
	if err != nil {
		return "", used, err
	}
	call := rt.meter("ask", "", completer, model, askReq)
	var b strings.Builder
	for d := range ch {
		call.see(d)
		b.WriteString(d.Content)
	}
	call.done()
	return b.String(), used, ctx.Err()
}

const basePrompt = "You are Rove Code, a local coding agent. Prefer tools over speculation. Stay inside the workspace. Do not invent files that are not there."

// paths tells the agent where it is. Without it a model asked to write a
// file invents a plausible home directory ("/Users/someone/...") and the
// write fails; the folders it may use are cheap to state and stop the guess.
func paths(workspace string) string {
	b := strings.Builder{}
	b.WriteString("\n\n## Paths\n")
	if workspace != "" {
		b.WriteString("Workspace: " + workspace + "\n")
		if name := filepath.Base(workspace); name != "" && name != "." && name != "/" {
			b.WriteString("Project: " + name + "\n")
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		b.WriteString("Home: " + home + "\n")
	}
	b.WriteString("Use these paths. Never invent a path, a user name or a home directory; if you need one that is not here, find it with a tool first.")
	return b.String()
}

// StyleRule is how every agent writes: plain working notes, not a chatty
// assistant. Models default to cheerful replies full of emoji; the app shows
// their text as a work log, so it asks for the tone of one.
const StyleRule = "\n\nHow you write: plain, short working notes, like a senior engineer's log. No emojis, no exclamation marks, no greetings, pleasantries or filler, and do not repeat the question back. Before each tool call, say in one short sentence what you are about to check or do and why; after it, state what you found in a sentence or two. Use lists and tables only when they make things clearer. Write a table as a Markdown pipe table with a header and a |---|---| rule line, never inside a fenced block: fences are for code and command output only, and the app renders them as code. End with the result or the one question you need answered."

func (rt *Runtime) buildMessages(ctx context.Context, agent types.Agent, req RunRequest, ps Persona) ([]provider.ChatMessage, error) {
	sys := agent.SystemPrompt
	if sys == "" {
		sys = basePrompt
	}
	if ps.Prompt != "" {
		// the session's character replaces the agent's generic identity
		sys = "You work inside Rove Code, a local coding app, with tools on the user's workspace. Prefer tools over speculation; do not invent files.\n\n" + ps.Prompt
	}
	sys += StyleRule
	// the project's own rules come right after who the agent is: they are
	// stable, so they sit in the cached part of the prompt
	if rules, _ := ProjectRules(req.Workspace); rules != "" {
		sys += rules
	}
	sys += paths(req.Workspace)
	sys += environment()
	if req.SystemExtra != "" {
		sys += "\n\n" + req.SystemExtra
	}
	if rt.memory != nil {
		sys += rt.memory.PromptBlock(ctx, agent.ID, req.WorkspaceID, req.SessionID)
	}
	if ps.Context != "" {
		sys += "\n\n" + ps.Context
	}
	out := []provider.ChatMessage{{Role: "system", Content: sys}}
	if rt.sessions != nil && req.SessionID != "" {
		hist, err := rt.sessions.History(ctx, req.SessionID)
		if err != nil {
			return nil, err
		}
		// a compacted chat sends its summary instead of what it covers
		summary, rest := sinceSummary(hist)
		if summary != "" {
			out[0].Content += "\n\n## Earlier in this conversation (summary)\n" + summary
		}
		hist = trimHistory(rest, req.HistoryLimit)
		hist = lastTurns(hist, req.HistoryTurns)
		// images go again only with the last few user turns
		withImages := map[int]bool{}
		for i, n := len(hist)-1, 0; i >= 0 && n < imageTurns; i-- {
			if hist[i].Role == types.RoleUser {
				n++
				if len(hist[i].Images) > 0 {
					withImages[i] = true
				}
			}
		}
		for i, m := range hist {
			if m.Role == types.RoleSystem {
				continue
			}
			content := m.Content
			if i < len(hist)-1 && m.Role == types.RoleUser {
				content = CompactNotice(content)
			}
			if len(m.Images) > 0 && !withImages[i] {
				content += fmt.Sprintf("\n[%d image(s) attached earlier]", len(m.Images))
			}
			cm := provider.ChatMessage{Role: string(m.Role), Content: content, ToolCalls: m.ToolCalls}
			if withImages[i] {
				cm.Images = m.Images
			}
			if m.ToolResult != nil {
				cm.ToolCallID = m.ToolResult.ToolCallID
			}
			out = append(out, cm)
		}
	} else if req.UserMessage != "" {
		out = append(out, provider.ChatMessage{Role: "user", Content: req.UserMessage, Images: req.Images})
	}
	return out, nil
}

func (rt *Runtime) emitStatus(a types.Agent) {
	if rt.bus == nil {
		return
	}
	rt.bus.Publish(types.Event{
		Type:  types.EventAgentStatus,
		Topic: "agent." + string(a.ID),
		Payload: map[string]any{
			"id":     string(a.ID),
			"status": string(a.Status),
		},
	})
}

func (rt *Runtime) EnsureDefault(ctx context.Context) (types.Agent, error) {
	all, err := rt.store.ListAgents(ctx)
	if err != nil {
		return types.Agent{}, err
	}
	if len(all) > 0 {
		return all[0], nil
	}
	return rt.Upsert(ctx, types.Agent{
		Name:     "default",
		Profile:  "default",
		Model:    "fake",
		Provider: "fake",
		Status:   types.AgentIdle,
	})
}

// ── tool tracking helpers ─────────────────────────────────────────────────────

// isFileEditTool returns true for built-in tool names that modify files.
func isFileEditTool(name string) bool {
	switch name {
	case "write_file", "patch_file", "create_file", "edit_file", "replace_file", "append_file":
		return true
	}
	return false
}

// isMutatingTool returns true for tool names that mutate the workspace:
// file writes, git commits, and shell execution.
func isMutatingTool(name string) bool {
	if isFileEditTool(name) {
		return true
	}
	switch name {
	case "shell", "run_command", "exec", "bash",
		"git_commit", "git_commit_all", "git_push":
		return true
	}
	return false
}

// extractPathArg extracts a "path" field from a JSON args blob.
func extractPathArg(argsJSON string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return ""
	}
	if p, ok := m["path"].(string); ok {
		return p
	}
	return ""
}

// noticePrefixes mark messages the map features inject into a session. They
// matter on the turn they arrive; afterwards only their headline does.
var noticePrefixes = []string{"[bağlam haritası", "[kod haritası"}

// CompactNotice shrinks an old map/relay notice to its headline and file
// list, dropping diffs and prose, so it does not cost tokens on every later
// turn. Other messages pass through unchanged.
func CompactNotice(content string) string {
	isNotice := false
	for _, p := range noticePrefixes {
		if strings.HasPrefix(content, p) {
			isNotice = true
			break
		}
	}
	if !isNotice || len(content) < 400 {
		return content
	}
	lines := strings.Split(content, "\n")
	out := []string{lines[0]}
	files := 0
	for _, ln := range lines[1:] {
		if strings.HasPrefix(ln, "```") {
			break
		}
		if strings.HasPrefix(ln, "- ") && files < 8 {
			out = append(out, ln)
			files++
		}
	}
	out = append(out, "(önceki bildirim; ayrıntılar kısaltıldı)")
	return strings.Join(out, "\n")
}

// trimHistory keeps the last limit messages, moving the cut forward to a
// user message so no tool result is left without its tool call.
// lastTurns keeps the last n user turns — each user message and everything
// after it — so a run's own tool calls and results are never cut.
func lastTurns(hist []types.Message, n int) []types.Message {
	if n <= 0 {
		return hist
	}
	seen := 0
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].Role == types.RoleUser {
			seen++
			if seen == n {
				return hist[i:]
			}
		}
	}
	return hist
}

func trimHistory(hist []types.Message, limit int) []types.Message {
	if limit <= 0 || len(hist) <= limit {
		return hist
	}
	start := len(hist) - limit
	for start < len(hist) && hist[start].Role != types.RoleUser {
		start++
	}
	if start >= len(hist) {
		return hist[len(hist)-1:]
	}
	return hist[start:]
}

// repeatNote marks a tool call that fails exactly as the same call already
// did in this run. Models stuck on a missing module or a syntax slip ran
// the same command five times; the note tells them to change something.
func repeatNote(failed map[string]string, key, out string, isErr bool) string {
	if !isErr {
		delete(failed, key)
		return out
	}
	if prev, ok := failed[key]; ok && prev == out {
		return out + "\n\n[rove] This exact call already failed the same way in this turn. Do not run it again unchanged: fix the cause (a missing program or module, the shell syntax, a wrong path) or tell the user what is blocking."
	}
	failed[key] = out
	return out
}
