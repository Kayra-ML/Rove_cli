package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Burak-ML/rove/internal/provider"
	"github.com/Burak-ML/rove/internal/tool"
	"github.com/Burak-ML/rove/internal/types"
)

func init() { turnRetryWait = time.Millisecond }

// scripted streams a fixed reply, then ends with err (nil = a clean end).
type scripted struct {
	parts []string
	err   error
	block bool // wait for the run to be cancelled before ending
}

func (*scripted) Kind() types.ProviderKind { return types.ProviderFake }
func (*scripted) Name() string             { return "scripted" }
func (s *scripted) Complete(ctx context.Context, _ provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	ch := make(chan provider.ChatDelta)
	go func() {
		defer close(ch)
		for _, p := range s.parts {
			ch <- provider.ChatDelta{Content: p}
		}
		if s.block {
			<-ctx.Done()
			return
		}
		ch <- provider.ChatDelta{Done: true, Err: s.err}
	}()
	return ch, nil
}

func scriptedRig(t *testing.T, s *scripted) (*Runtime, types.Agent, types.ID) {
	t.Helper()
	rt, ag, sess := policyRig(t, &stepModel{})
	rt.router.Register("scripted", s)
	ag.Provider = "scripted"
	ag, _ = rt.Upsert(context.Background(), ag)
	ses, _ := sess.Create(context.Background(), "s", ag.ID, "")
	return rt, ag, ses.ID
}

func TestBrokenStreamFailsTheRunAndKeepsItsText(t *testing.T) {
	rt, ag, sid := scriptedRig(t, &scripted{parts: []string{"half ", "an answer"}, err: errors.New("stream broke off")})
	ctx := context.Background()
	_, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: sid, UserMessage: "q"})
	if err == nil || !strings.Contains(err.Error(), "broke off") {
		t.Fatalf("err = %v", err)
	}
	hist, _ := rt.sessions.History(ctx, sid)
	if last := hist[len(hist)-1]; last.Role != types.RoleAssistant || last.Content != "half an answer" {
		t.Fatalf("last = %+v", last)
	}
	if a, _ := rt.Get(ctx, ag.ID); a.Status != types.AgentFailed {
		t.Fatalf("status %s", a.Status)
	}
}

func TestCancelledRunLeavesTheAgentIdle(t *testing.T) {
	rt, ag, sid := scriptedRig(t, &scripted{parts: []string{"working"}, block: true})
	done := make(chan map[string]any, 1)
	unsub := rt.bus.Subscribe(string(types.EventRunDone), func(ev types.Event) { done <- ev.Payload })
	defer unsub()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	_, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: sid, UserMessage: "q"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if a, _ := rt.Get(context.Background(), ag.ID); a.Status != types.AgentIdle {
		t.Fatalf("status %s", a.Status)
	}
	select {
	case p := <-done:
		if p["cancelled"] != true || p["error"] != nil {
			t.Fatalf("payload %v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no run.done")
	}
	hist, _ := rt.sessions.History(context.Background(), sid)
	if hist[len(hist)-1].Content != "working" {
		t.Fatal("text shown before the cancel was not kept")
	}
}

func TestStreamedTextIsCoalesced(t *testing.T) {
	parts := make([]string, 2000)
	for i := range parts {
		parts[i] = "x"
	}
	rt, ag, sid := scriptedRig(t, &scripted{parts: parts})
	var mu sync.Mutex
	var got strings.Builder
	var events int32
	unsub := rt.bus.Subscribe(string(types.EventMessageDelta), func(ev types.Event) {
		atomic.AddInt32(&events, 1)
		mu.Lock()
		got.WriteString(ev.Payload["delta"].(string))
		mu.Unlock()
	})
	defer unsub()
	if _, err := rt.Run(context.Background(), RunRequest{AgentID: ag.ID, SessionID: sid, UserMessage: "q"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // the bus delivers on its own goroutine
	mu.Lock()
	defer mu.Unlock()
	if got.String() != strings.Repeat("x", 2000) {
		t.Fatalf("text lost: %d bytes", got.Len())
	}
	if n := atomic.LoadInt32(&events); n > 200 {
		t.Fatalf("%d delta events for 2000 tokens", n)
	}
}

// bigTool returns far more text than a model should get back.
type bigTool struct{}

func (bigTool) Name() string                               { return "shell" }
func (bigTool) Description() string                        { return "run" }
func (bigTool) Parameters() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (bigTool) RequiredPermission() types.PermissionAction { return types.PermShell }
func (bigTool) Call(context.Context, tool.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "begin\n" + strings.Repeat("log line\n", 50000) + "the error at the end"}, nil
}

func TestHugeToolOutputIsCut(t *testing.T) {
	m := &stepModel{steps: [][]types.ToolCall{{{ID: "1", Name: "shell", ArgsJSON: `{}`}}}}
	rt, ag, sess := policyRig(t, m, bigTool{})
	ctx := context.Background()
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "x"}); err != nil {
		t.Fatal(err)
	}
	hist, _ := sess.History(ctx, s.ID)
	for _, msg := range hist {
		if msg.Role == types.RoleTool {
			if len(msg.Content) > tool.MaxOutput+300 || !strings.HasSuffix(msg.Content, "the error at the end") {
				t.Fatalf("tool result %d bytes", len(msg.Content))
			}
			return
		}
	}
	t.Fatal("no tool result")
}

func TestEveryTurnSeesTheWholeRun(t *testing.T) {
	m := &stepModel{steps: [][]types.ToolCall{
		{{ID: "1", Name: "shell", ArgsJSON: `{"n":1}`}},
		{{ID: "2", Name: "shell", ArgsJSON: `{"n":2}`}},
	}}
	rt, ag, sess := policyRig(t, m, bigTool{})
	ctx := context.Background()
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "x"}); err != nil {
		t.Fatal(err)
	}
	// the third call carries both tool calls and both results, in order
	var roles []string
	for _, msg := range m.reqs[2].Messages[1:] {
		roles = append(roles, msg.Role)
	}
	if got := strings.Join(roles, ","); got != "user,assistant,tool,assistant,tool" {
		t.Fatalf("roles %s", got)
	}
}

func TestTurnLimitIsSaid(t *testing.T) {
	steps := make([][]types.ToolCall, defaultMaxTurns+1)
	for i := range steps {
		steps[i] = []types.ToolCall{{ID: "c", Name: "list_dir", ArgsJSON: `{}`}}
	}
	rt, ag, sess := policyRig(t, &stepModel{steps: steps}, tool.ListDir{})
	ctx := context.Background()
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	res, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "x", Workspace: t.TempDir()})
	if err != nil || res.Done {
		t.Fatalf("%+v %v", res, err)
	}
	hist, _ := sess.History(ctx, s.ID)
	if last := hist[len(hist)-1]; !strings.Contains(last.Content, "Tur sınırına") {
		t.Fatalf("last = %q", last.Content)
	}
}

func TestOldToolResultsShortenInSteps(t *testing.T) {
	var hist []types.Message
	hist = append(hist, types.Message{Role: types.RoleUser, Content: "go"})
	for i := 0; i < 20; i++ {
		hist = append(hist,
			types.Message{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "x", Name: "read_file"}}},
			types.Message{Role: types.RoleTool, Content: strings.Repeat("y", 4000)})
	}
	if decayedTools(hist, 0) != nil || decayedTools(hist, 1_000_000) != nil {
		t.Fatal("shortened under budget")
	}
	marked := decayedTools(hist, 1000)
	// 20 results, 6 kept whole, shortened in steps of 8 → 8
	if len(marked) != 8 {
		t.Fatalf("%d marked", len(marked))
	}
	for i := range marked {
		if i > 20 {
			t.Fatalf("a recent result (%d) was marked", i)
		}
	}
}

// flaky breaks off its first reply mid-stream, then answers whole.
type flaky struct {
	mu    sync.Mutex
	calls int
}

func (*flaky) Kind() types.ProviderKind { return types.ProviderFake }
func (*flaky) Name() string             { return "flaky" }
func (f *flaky) Complete(ctx context.Context, _ provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	ch := make(chan provider.ChatDelta, 4)
	if n == 1 {
		ch <- provider.ChatDelta{Content: "half "}
		ch <- provider.ChatDelta{Done: true, Err: errors.New("stream broke off")}
	} else {
		ch <- provider.ChatDelta{Content: "the whole answer"}
		ch <- provider.ChatDelta{Done: true}
	}
	close(ch)
	return ch, nil
}

func TestABrokenReplyIsAskedForAgain(t *testing.T) {
	rt, ag, sess := policyRig(t, &stepModel{})
	f := &flaky{}
	rt.router.Register("flaky", f)
	ag.Provider = "flaky"
	ag, _ = rt.Upsert(context.Background(), ag)
	ctx := context.Background()
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	resets := make(chan struct{}, 4)
	unsub := rt.bus.Subscribe(string(EventMessageReset), func(types.Event) { resets <- struct{}{} })
	defer unsub()
	res, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "q"})
	if err != nil || !res.Done || res.Assistant != "the whole answer" {
		t.Fatalf("%+v %v", res, err)
	}
	if f.calls != 2 {
		t.Fatalf("%d calls", f.calls)
	}
	select {
	case <-resets:
	case <-time.After(2 * time.Second):
		t.Fatal("the half reply shown was not taken back")
	}
	hist, _ := sess.History(ctx, s.ID)
	var replies []string
	for _, m := range hist {
		if m.Role == types.RoleAssistant {
			replies = append(replies, m.Content)
		}
	}
	if len(replies) != 1 || replies[0] != "the whole answer" {
		t.Fatalf("replies %q", replies)
	}
}
