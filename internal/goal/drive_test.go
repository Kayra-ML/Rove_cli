package goal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/judge"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

// scripted plays both the goal's agent and its reviewer. Reviewer calls are
// the tool-less ones whose system prompt is a reviewer's.
type scripted struct {
	mu      sync.Mutex
	agent   func(ctx context.Context, n int, req provider.ChatRequest) ([]provider.ChatDelta, error)
	review  func(user string) string
	calls   []provider.ChatRequest // agent calls
	reviews []string               // reviewer user messages
}

func (*scripted) Kind() types.ProviderKind { return types.ProviderFake }
func (*scripted) Name() string             { return "scripted" }
func (s *scripted) Complete(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	var out []provider.ChatDelta
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, `{"met": [criterion numbers]`) && len(req.Tools) == 0 && len(req.Messages) == 2 {
		user := req.Messages[1].Content
		s.mu.Lock()
		s.reviews = append(s.reviews, user)
		s.mu.Unlock()
		out = []provider.ChatDelta{{Content: s.review(user)}}
	} else {
		s.mu.Lock()
		s.calls = append(s.calls, req)
		n := len(s.calls)
		s.mu.Unlock()
		var err error
		out, err = s.agent(ctx, n, req)
		if err != nil {
			return nil, err
		}
	}
	ch := make(chan provider.ChatDelta, len(out)+1)
	for _, d := range out {
		ch <- d
	}
	ch <- provider.ChatDelta{Done: true}
	close(ch)
	return ch, nil
}

func (s *scripted) agentCalls() []provider.ChatRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]provider.ChatRequest{}, s.calls...)
}

func (s *scripted) reviewed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.reviews...)
}

type rig struct {
	e    *Engine
	st   *store.Store
	sess *session.Manager
	ag   types.Agent
	m    *scripted
}

func newRig(t *testing.T, m *scripted) *rig {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	r := provider.NewRouter()
	r.Register("scripted", m)
	sess := session.New(st, bus)
	tools := tool.New(nil)
	for _, tl := range []tool.Tool{tool.ReadFile{}, tool.WriteFile{}, tool.PatchFile{}, tool.ListDir{}, tool.Shell{}} {
		tools.Register(tl)
	}
	tools.Register(fakeWeb{})
	rt := agent.New(st, bus, sess, nil, r, tools)
	ag, err := rt.Upsert(context.Background(), types.Agent{Name: "a", Provider: "scripted", Model: "m1"})
	if err != nil {
		t.Fatal(err)
	}
	j := judge.New(nil)
	j.Ask = rt.Ask
	e := New(st, bus, rt, j, nil)
	t.Cleanup(e.Close)
	e.ToolNames = func() []string {
		var out []string
		for _, s := range tools.Specs() {
			out = append(out, s.Name)
		}
		return out
	}
	return &rig{e: e, st: st, sess: sess, ag: ag, m: m}
}

// fakeWeb is a tool a coding goal must not be offered.
type fakeWeb struct{}

func (fakeWeb) Name() string                               { return "web_fetch" }
func (fakeWeb) Description() string                        { return "fetch a page" }
func (fakeWeb) Parameters() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (fakeWeb) RequiredPermission() types.PermissionAction { return types.PermNetwork }
func (fakeWeb) Call(context.Context, tool.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "page"}, nil
}

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

func text(s string) []provider.ChatDelta { return []provider.ChatDelta{{Content: s}} }

func toolNames(req provider.ChatRequest) map[string]bool {
	out := map[string]bool{}
	for _, t := range req.Tools {
		out[t.Name] = true
	}
	return out
}

func lastUser(req provider.ChatRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return req.Messages[i].Content
		}
	}
	return ""
}

// A goal set in a chat works in that chat, with its agent, in its folder —
// no hidden worktree — and a reviewer that reads the real changes decides
// it is done.
func TestChatGoalRunsWithTheChatsAgentInItsFolder(t *testing.T) {
	m := &scripted{}
	m.agent = func(_ context.Context, n int, req provider.ChatRequest) ([]provider.ChatDelta, error) {
		if n == 1 {
			return []provider.ChatDelta{{ToolCalls: []types.ToolCall{{ID: "c1", Name: "write_file", ArgsJSON: `{"path":"README.md","content":"# Kurulum\n1. go build\n"}`}}}}, nil
		}
		return text("README yazıldı."), nil
	}
	m.review = func(user string) string {
		if strings.Contains(user, "README.md") {
			return `{"met":[1],"missing":[],"reason":"README lists install steps"}`
		}
		return `{"met":[],"missing":[{"n":1,"why":"no README"}],"reason":"nothing changed"}`
	}
	r := newRig(t, m)
	ctx := context.Background()
	dir := gitRepo(t)
	chat, _ := r.sess.CreateIn(ctx, types.SpaceChat, "c", r.ag.ID, "")

	// the chat sends no agent: the goal takes the chat's
	g, err := r.e.Create(ctx, types.Goal{Title: "README yaz", SessionID: chat.ID, CompletionContract: types.CompletionContract{Criteria: []string{"kurulum adımları var"}}})
	if err != nil {
		t.Fatal(err)
	}
	if g.AgentID != r.ag.ID {
		t.Fatalf("goal agent = %q", g.AgentID)
	}
	g, err = r.e.Drive(ctx, g.ID, "", dir)
	if err != nil || g.Status != types.GoalDone || g.Iteration != 1 {
		t.Fatalf("drive = %v %s it=%d verdict=%+v", err, g.Status, g.Iteration, g.LastVerdict)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); !strings.Contains(string(b), "Kurulum") {
		t.Fatal("the change is not in the chat's folder")
	}
	if _, err := os.Stat(filepath.Join(dir, ".aether")); err == nil {
		t.Fatal("a worktree was made for a chat goal")
	}
	// the coding tool policy is applied: no web tool offered
	first := r.m.agentCalls()[0]
	if names := toolNames(first); !names["write_file"] || names["web_fetch"] {
		t.Fatalf("tools offered = %v", names)
	}
	if rv := r.m.reviewed(); len(rv) != 1 || !strings.Contains(rv[0], "README.md") {
		t.Fatalf("reviewer saw %q", rv)
	}
	if !strings.Contains(g.LastVerdict.Reason, "README lists install steps") {
		t.Fatalf("verdict = %q", g.LastVerdict.Reason)
	}
	if msgs, _ := r.st.ListMessages(ctx, chat.ID); len(msgs) == 0 {
		t.Fatal("the goal did not work in the chat")
	}
}

// Saying "done" and repeating the criterion is not evidence. The goal runs
// exactly MaxIterations agent runs (not one more), later iterations get a
// short prompt, and each sees only its last few chat turns.
func TestClaimsDoNotFinishAGoalAndIterationsStayCheap(t *testing.T) {
	m := &scripted{}
	m.agent = func(context.Context, int, provider.ChatRequest) ([]provider.ChatDelta, error) {
		return text("Bitti! kurulum adımları var."), nil
	}
	m.review = func(user string) string {
		if strings.Contains(user, "Changed files:\n(none)") {
			return `{"met":[],"missing":[{"n":1,"why":"nothing changed in the workspace"}],"reason":"only a claim"}`
		}
		return `{"met":[1],"missing":[],"reason":"ok"}`
	}
	r := newRig(t, m)
	ctx := context.Background()
	dir := gitRepo(t)
	chat, _ := r.sess.CreateIn(ctx, types.SpaceChat, "c", r.ag.ID, "")
	// an old conversation in the chat
	for i := 0; i < 5; i++ {
		_, _ = r.sess.Append(ctx, types.Message{SessionID: chat.ID, Role: types.RoleUser, Content: "eski soru"})
		_, _ = r.sess.Append(ctx, types.Message{SessionID: chat.ID, Role: types.RoleAssistant, Content: "eski cevap"})
	}
	g, _ := r.e.Create(ctx, types.Goal{Title: "README yaz", SessionID: chat.ID, CompletionContract: types.CompletionContract{Criteria: []string{"kurulum adımları var"}, MaxIterations: 3}})
	g, err := r.e.Drive(ctx, g.ID, chat.ID, dir)
	if err == nil || g.Status != types.GoalBlocked {
		t.Fatalf("status = %s err=%v", g.Status, err)
	}
	calls := r.m.agentCalls()
	if len(calls) != 3 {
		t.Fatalf("agent ran %d times for 3 iterations", len(calls))
	}
	if !strings.Contains(g.LastVerdict.Reason, "max iterations") {
		t.Fatalf("verdict = %q", g.LastVerdict.Reason)
	}
	first, second := lastUser(calls[0]), lastUser(calls[1])
	if !strings.Contains(first, "Completion criteria:") {
		t.Fatalf("first prompt lacks the brief:\n%s", first)
	}
	if strings.Contains(second, "Completion criteria:") || !strings.Contains(second, "Reviewer: ") {
		t.Fatalf("second prompt is not the short follow-up:\n%s", second)
	}
	// three user turns of the chat at most; after two identical iterations
	// the harness sees no progress and widens that (LargeContext) to six
	for i, c := range calls {
		users := 0
		for _, msg := range c.Messages {
			if msg.Role == "user" {
				users++
			}
		}
		limit := 3
		if i == 2 {
			limit = 6
		}
		if users > limit {
			t.Fatalf("iteration %d replayed %d user turns", i+1, users)
		}
	}
	if muts, _ := r.st.ListHarnessMutations(ctx, g.ID); len(muts) == 0 || !strings.Contains(muts[0].Reason, "no progress") {
		t.Fatalf("mutations = %+v", muts)
	}
}

// Goals share the engine: two can run at once (run with -race), /stop in a
// chat stops only its goal, and a stopped goal resumes from its checkpoint.
func TestGoalsRunConcurrentlyStopPerChatAndResume(t *testing.T) {
	release := make(chan struct{})
	m := &scripted{}
	m.agent = func(ctx context.Context, _ int, req provider.ChatRequest) ([]provider.ChatDelta, error) {
		p := lastUser(req)
		switch {
		case strings.Contains(p, "Goal: bir") && strings.Contains(p, "Iteration 1 "):
			return text("ilk adım yapıldı"), nil
		case strings.Contains(p, "Goal: bir") && strings.Contains(p, "Resuming"):
			return text("devam edildi"), nil
		}
		select { // everything else waits
		case <-release:
			return text("tamam"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	m.review = func(user string) string {
		if strings.Contains(user, "devam edildi") || strings.Contains(user, "tamam") {
			return `{"met":[1],"missing":[],"reason":"ok"}`
		}
		return `{"met":[],"missing":[{"n":1,"why":"not yet"}],"reason":"not yet"}`
	}
	r := newRig(t, m)
	ctx := context.Background()
	c1, _ := r.sess.CreateIn(ctx, types.SpaceChat, "c1", r.ag.ID, "")
	c2, _ := r.sess.CreateIn(ctx, types.SpaceChat, "c2", r.ag.ID, "")
	g1, _ := r.e.Create(ctx, types.Goal{Title: "bir", SessionID: c1.ID, CompletionContract: types.CompletionContract{Criteria: []string{"x"}}})
	g2, _ := r.e.Create(ctx, types.Goal{Title: "iki", SessionID: c2.ID, CompletionContract: types.CompletionContract{Criteria: []string{"y"}}})
	if err := r.e.Start(g1.ID, c1.ID, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := r.e.Start(g2.ID, c2.ID, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return r.e.Running(g1.ID) && r.e.Running(g2.ID) })
	if err := r.e.Start(g1.ID, c1.ID, ""); !errors.Is(err, ErrRunning) {
		t.Fatalf("started a running goal twice: %v", err)
	}
	// g1 finished iteration 1 and waits in 2
	waitFor(t, func() bool {
		for _, c := range r.m.agentCalls() {
			if p := lastUser(c); strings.Contains(p, "Goal: bir") && strings.Contains(p, "Iteration 2 ") {
				return true
			}
		}
		return false
	})

	if n := r.e.CancelSession(c1.ID); n != 1 {
		t.Fatalf("canceled %d goals", n)
	}
	waitFor(t, func() bool { g, _ := r.st.GetGoal(ctx, g1.ID); return g.Status == types.GoalCanceled })
	if !r.e.Running(g2.ID) {
		t.Fatal("stopping one chat stopped the other's goal")
	}
	close(release)
	waitFor(t, func() bool { g, _ := r.st.GetGoal(ctx, g2.ID); return g.Status == types.GoalDone })

	// the stopped goal kept its checkpoint and resumes where it was
	if body, err := r.st.GetGoalCheckpoint(ctx, g1.ID); err != nil || !strings.Contains(body, "ilk adım") {
		t.Fatalf("checkpoint = %q %v", body, err)
	}
	g, err := r.e.Drive(ctx, g1.ID, "", t.TempDir())
	if err != nil || g.Status != types.GoalDone || g.Iteration != 3 {
		t.Fatalf("resume = %v %s it=%d", err, g.Status, g.Iteration)
	}
	if _, err := r.st.GetGoalCheckpoint(ctx, g1.ID); err == nil {
		t.Fatal("checkpoint kept after the goal was done")
	}
}

// A failed run is retried (the recovery plan), and a goal with no agent
// stops at once instead of spinning through empty iterations.
func TestRetryAndNoAgent(t *testing.T) {
	m := &scripted{}
	m.agent = func(_ context.Context, n int, _ provider.ChatRequest) ([]provider.ChatDelta, error) {
		if n == 1 {
			return nil, errors.New("rate limited")
		}
		return text("yaptım"), nil
	}
	m.review = func(string) string { return `{"met":[1],"missing":[],"reason":"ok"}` }
	r := newRig(t, m)
	ctx := context.Background()
	chat, _ := r.sess.CreateIn(ctx, types.SpaceChat, "c", r.ag.ID, "")
	g, _ := r.e.Create(ctx, types.Goal{Title: "t", SessionID: chat.ID, CompletionContract: types.CompletionContract{Criteria: []string{"x"}}})
	g, err := r.e.Drive(ctx, g.ID, "", t.TempDir())
	if err != nil || g.Status != types.GoalDone || len(r.m.agentCalls()) != 2 {
		t.Fatalf("retry = %v %s calls=%d", err, g.Status, len(r.m.agentCalls()))
	}
	if !strings.Contains(lastUser(r.m.agentCalls()[1]), "The previous attempt failed: rate limited") {
		t.Fatal("the retry was not told what failed")
	}

	orphan, _ := r.e.Create(ctx, types.Goal{Title: "sahipsiz", CompletionContract: types.CompletionContract{Criteria: []string{"x"}}})
	orphan, err = r.e.Drive(ctx, orphan.ID, "", "")
	if err == nil || orphan.Status != types.GoalBlocked || orphan.Iteration != 0 {
		t.Fatalf("no-agent goal = %s it=%d", orphan.Status, orphan.Iteration)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
