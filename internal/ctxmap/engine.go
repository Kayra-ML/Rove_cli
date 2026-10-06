// Package ctxmap is the context map: one global canvas of sessions joined
// by cables. A cable keeps two sessions consistent — when one changes files,
// the other is told what changed and asked to mirror it if it has a
// counterpart (same logo on the website and the desktop app, same API type
// on server and client).
//
// Token cost is the main design constraint. A change only reaches a model
// when a zero-token pre-check finds a counterpart in the far workspace;
// bursts are coalesced into one notice; notices carry a clipped +/- diff and
// the candidate files; relay runs get few turns and a trimmed history; and
// changes travel one hop by default.
package ctxmap

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

// Runner is the slice of agent.Runtime the engine needs.
type Runner interface {
	Run(ctx context.Context, req agent.RunRequest) (agent.RunResult, error)
	Busy(sessionID types.ID) bool
}

const (
	KindAuto   = "auto"
	KindManual = "manual"
	KindAgent  = "agent"

	StatusQueued  = "queued"
	StatusWaiting = "waiting"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"

	maxMessageRunes = 2000
	diffBudget      = 1500
)

type Engine struct {
	st     *store.Store
	bus    *eventbus.Bus
	run    Runner
	finder Finder

	// MaxHops bounds how far one change travels: A→B is hop 1. The default
	// of 1 means B's mirrored edit is not forwarded on to C.
	MaxHops int
	// Debounce coalesces changes from one session to another that arrive
	// within this window into a single notice.
	Debounce time.Duration
	// RelayTurns and RelayHistory cap what a relay-triggered run may spend.
	RelayTurns   int
	RelayHistory int
	// WaitBusy is how long a relay waits for a busy target session.
	WaitBusy time.Duration
	// AgentBudget caps agent-initiated messages per session pair per window.
	AgentBudget int
	AgentWindow time.Duration
	poll        time.Duration

	mu       sync.Mutex
	pending  map[string]*pending
	agentLog map[string][]time.Time
	wg       sync.WaitGroup
}

type pending struct {
	relay     types.Relay
	link      types.SessionLink
	from      types.Session
	root      string
	chain     []types.ID
	assistant string
}

func New(st *store.Store, bus *eventbus.Bus, run Runner, finder Finder) *Engine {
	return &Engine{
		st: st, bus: bus, run: run, finder: finder,
		MaxHops: 1, Debounce: 6 * time.Second,
		RelayTurns: 6, RelayHistory: 10,
		WaitBusy:    15 * time.Minute,
		AgentBudget: 4, AgentWindow: 10 * time.Minute,
		poll:     1500 * time.Millisecond,
		pending:  map[string]*pending{},
		agentLog: map[string][]time.Time{},
	}
}

// Wait blocks until every relay started so far has finished. Tests use it.
func (e *Engine) Wait() { e.wg.Wait() }

// OnRunDone is the agent run hook: a finished turn that edited files is
// queued along every automatic cable that flows away from its session.
func (e *Engine) OnRunDone(req agent.RunRequest, res agent.RunResult) {
	if req.SessionID == "" || req.Hop >= e.MaxHops {
		return
	}
	files := uniq(res.FilesEdited)
	if len(files) == 0 {
		return
	}
	ctx := context.Background()
	links, err := e.st.ListSessionLinks(ctx, req.SessionID)
	if err != nil || len(links) == 0 {
		return
	}
	from, err := e.st.GetSession(ctx, req.SessionID)
	if err != nil {
		return
	}
	root := req.Workspace
	if root == "" {
		root = e.workspacePath(ctx, from.WorkspaceID)
	}
	chain := append(append([]types.ID(nil), req.Chain...), req.SessionID)
	for _, l := range links {
		to := l.Other(req.SessionID)
		if !l.Auto || !l.Flows(req.SessionID, to) || contains(chain, to) {
			continue
		}
		e.queueAuto(l, from, to, root, files, chain, req.Hop+1, res.Assistant)
	}
}

// queueAuto either merges files into a notice already waiting for the same
// pair or starts a new one that fires after the debounce window.
func (e *Engine) queueAuto(l types.SessionLink, from types.Session, to types.ID, root string, files []string, chain []types.ID, hop int, assistant string) {
	key := string(from.ID) + ">" + string(to)
	e.mu.Lock()
	if p, ok := e.pending[key]; ok {
		p.relay.Files = uniq(append(p.relay.Files, files...))
		p.assistant = assistant
		r := p.relay
		e.mu.Unlock()
		if r, err := e.st.PutRelay(context.Background(), r); err == nil {
			e.publish(r)
		}
		return
	}
	r, err := e.st.PutRelay(context.Background(), types.Relay{
		LinkID: l.ID, From: from.ID, To: to, Kind: KindAuto, Status: StatusQueued, Hop: hop, Files: files,
	})
	if err != nil {
		e.mu.Unlock()
		return
	}
	p := &pending{relay: r, link: l, from: from, root: root, chain: chain, assistant: assistant}
	e.pending[key] = p
	e.wg.Add(1)
	e.mu.Unlock()
	e.publish(r)
	go func() {
		defer e.wg.Done()
		time.Sleep(e.Debounce)
		e.mu.Lock()
		delete(e.pending, key)
		e.mu.Unlock()
		e.deliverAuto(p)
	}()
}

func (e *Engine) deliverAuto(p *pending) {
	ctx := context.Background()
	r := p.relay
	raw := sourceDiff(ctx, p.root, r.Files)
	target, err := e.st.GetSession(ctx, r.To)
	if err != nil {
		e.fail(r, fmt.Errorf("target session: %w", err))
		return
	}
	targetRoot := e.workspacePath(ctx, target.WorkspaceID)
	if p.link.Mode != types.LinkAlways && e.finder != nil {
		r.Matches = e.finder.Find(ctx, targetRoot, r.Files, raw)
		if len(r.Matches) == 0 {
			r.Status, r.Tokens = StatusSkipped, 0
			r.Summary = "Karşılık bulunamadı; model çağrılmadı."
			r, _ = e.st.PutRelay(ctx, r)
			e.publish(r)
			return
		}
	}
	msg := changeNotice(p.from, p.root, r.Files, compactDiff(raw, diffBudget), r.Matches, p.assistant, r.Hop)
	e.deliver(r, target, targetRoot, msg, p.chain)
}

// Send carries a message from one session to another along their cable.
// kind is KindManual (a person typed it) or KindAgent (a tool call). An
// explicit message skips the pre-check: someone decided it matters.
func (e *Engine) Send(ctx context.Context, from, to types.ID, content, kind string) (types.Relay, error) {
	content = clip(strings.TrimSpace(content), maxMessageRunes)
	if content == "" {
		return types.Relay{}, fmt.Errorf("empty message")
	}
	link, ok := e.linkBetween(ctx, from, to)
	if !ok {
		return types.Relay{}, fmt.Errorf("these sessions are not connected on the context map")
	}
	if kind == KindAgent && !e.allowAgentMessage(from, to) {
		return types.Relay{}, fmt.Errorf("message budget exhausted for this pair; try again later")
	}
	src, err := e.st.GetSession(ctx, from)
	if err != nil {
		return types.Relay{}, err
	}
	target, err := e.st.GetSession(ctx, to)
	if err != nil {
		return types.Relay{}, err
	}
	who := "kullanıcı"
	if kind == KindAgent {
		who = "agent"
	}
	msg := fmt.Sprintf("[bağlam haritası · %q oturumundan mesaj (%s)]\n%s", title(src), who, content)
	r, err := e.st.PutRelay(ctx, types.Relay{
		LinkID: link.ID, From: from, To: to, Kind: kind, Status: StatusQueued, Hop: 1, Summary: clip(content, 280),
	})
	if err != nil {
		return r, err
	}
	e.publish(r)
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.deliver(r, target, e.workspacePath(context.Background(), target.WorkspaceID), msg, []types.ID{from})
	}()
	return r, nil
}

// deliver runs the target session with msg, waiting first if it is busy so
// a person's run is never cut off.
func (e *Engine) deliver(r types.Relay, target types.Session, root, msg string, chain []types.ID) {
	ctx := context.Background()
	r.Tokens = estimateTokens(msg)
	if e.run.Busy(target.ID) {
		r.Status = StatusWaiting
		r, _ = e.st.PutRelay(ctx, r)
		e.publish(r)
		deadline := time.Now().Add(e.WaitBusy)
		for e.run.Busy(target.ID) {
			if time.Now().After(deadline) {
				e.fail(r, fmt.Errorf("target session stayed busy"))
				return
			}
			time.Sleep(e.poll)
		}
	}
	r.Status = StatusRunning
	r, _ = e.st.PutRelay(ctx, r)
	e.publish(r)
	res, err := e.run.Run(ctx, agent.RunRequest{
		AgentID:      target.AgentID,
		SessionID:    target.ID,
		WorkspaceID:  target.WorkspaceID,
		Workspace:    root,
		UserMessage:  msg,
		Chain:        chain,
		Hop:          r.Hop,
		MaxTurns:     e.RelayTurns,
		HistoryLimit: e.RelayHistory,
	})
	if err != nil {
		e.fail(r, err)
		return
	}
	r.Status = StatusDone
	if s := strings.TrimSpace(res.Assistant); s != "" {
		r.Summary = clip(s, 280)
	}
	r, _ = e.st.PutRelay(ctx, r)
	e.publish(r)
}

func (e *Engine) fail(r types.Relay, err error) {
	r.Status, r.Error = StatusFailed, err.Error()
	r, _ = e.st.PutRelay(context.Background(), r)
	e.publish(r)
}

// changeNotice is what the far session receives: the changed files, a
// clipped +/- diff, where its own counterpart probably is, and the rule to
// mirror only where a counterpart exists.
func changeNotice(from types.Session, root string, files []string, diff string, matches []string, assistant string, hop int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[bağlam haritası · otomatik] Bağlı %q oturumu", title(from))
	if root != "" {
		fmt.Fprintf(&b, " (%s)", filepath.Base(root))
	}
	b.WriteString(" şu dosyaları değiştirdi:\n")
	for i, f := range files {
		if i >= 12 {
			fmt.Fprintf(&b, "- … +%d dosya\n", len(files)-i)
			break
		}
		b.WriteString("- " + f + "\n")
	}
	if s := strings.TrimSpace(assistant); s != "" {
		b.WriteString("Özet: " + clip(firstLine(s), 240) + "\n")
	}
	if len(matches) > 0 {
		b.WriteString("Sendeki olası karşılıklar (önce bunlara bak): " + strings.Join(matches, ", ") + "\n")
	}
	if diff != "" {
		b.WriteString("```diff\n" + diff + "\n```\n")
	}
	b.WriteString("Karşılığı varsa aynı değişikliği burada uygula; yoksa hiçbir şeyi değiştirme ve yalnızca \"etkilenmedi\" yaz. Kısa tut.")
	return b.String()
}

func (e *Engine) linkBetween(ctx context.Context, a, b types.ID) (types.SessionLink, bool) {
	links, err := e.st.ListSessionLinks(ctx, a)
	if err != nil {
		return types.SessionLink{}, false
	}
	for _, l := range links {
		if l.Other(a) == b {
			return l, true
		}
	}
	return types.SessionLink{}, false
}

func (e *Engine) allowAgentMessage(from, to types.ID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := string(from) + ">" + string(to)
	now := time.Now()
	var keep []time.Time
	for _, t := range e.agentLog[key] {
		if now.Sub(t) < e.AgentWindow {
			keep = append(keep, t)
		}
	}
	if len(keep) >= e.AgentBudget {
		e.agentLog[key] = keep
		return false
	}
	e.agentLog[key] = append(keep, now)
	return true
}

func (e *Engine) workspacePath(ctx context.Context, wsID types.ID) string {
	if wsID == "" {
		return ""
	}
	ws, err := e.st.GetWorkspace(ctx, wsID)
	if err != nil {
		return ""
	}
	return ws.Path
}

func (e *Engine) publish(r types.Relay) {
	if e.bus == nil {
		return
	}
	e.bus.Publish(types.Event{
		Type:  types.EventRelay,
		Topic: "ctxmap",
		Payload: map[string]any{
			"id": string(r.ID), "linkId": string(r.LinkID), "from": string(r.From), "to": string(r.To),
			"kind": r.Kind, "status": r.Status, "hop": r.Hop, "files": r.Files, "matches": r.Matches,
			"tokens": r.Tokens, "summary": r.Summary, "error": r.Error,
		},
	})
}

// estimateTokens is the usual ~4 characters per token rule of thumb.
func estimateTokens(s string) int {
	return (len([]rune(s)) + 3) / 4
}

func title(s types.Session) string {
	if t := strings.TrimSpace(s.Title); t != "" {
		return t
	}
	return string(s.ID)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func contains(ids []types.ID, id types.ID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
