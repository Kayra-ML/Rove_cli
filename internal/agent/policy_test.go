package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

// stepModel answers each call with the next step: tool calls, then text.
type stepModel struct {
	mu    sync.Mutex
	steps [][]types.ToolCall
	reqs  []provider.ChatRequest
}

func (*stepModel) Kind() types.ProviderKind { return types.ProviderFake }
func (*stepModel) Name() string             { return "step" }
func (m *stepModel) Complete(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	m.mu.Lock()
	n := len(m.reqs)
	m.reqs = append(m.reqs, req)
	m.mu.Unlock()
	ch := make(chan provider.ChatDelta, 2)
	if n < len(m.steps) {
		ch <- provider.ChatDelta{ToolCalls: m.steps[n]}
	} else {
		ch <- provider.ChatDelta{Content: "ok"}
	}
	ch <- provider.ChatDelta{Done: true}
	close(ch)
	return ch, nil
}

// slowRead is a read-only tool that tracks how many run at once.
type slowRead struct{ now, peak *int32 }

func (slowRead) Name() string                               { return "read_file" }
func (slowRead) Description() string                        { return "read" }
func (slowRead) Parameters() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (slowRead) RequiredPermission() types.PermissionAction { return types.PermFilesystem }
func (r slowRead) Call(context.Context, tool.Context, json.RawMessage) (tool.Result, error) {
	n := atomic.AddInt32(r.now, 1)
	for {
		p := atomic.LoadInt32(r.peak)
		if n <= p || atomic.CompareAndSwapInt32(r.peak, p, n) {
			break
		}
	}
	time.Sleep(40 * time.Millisecond)
	atomic.AddInt32(r.now, -1)
	return tool.Result{Content: "text"}, nil
}

func policyRig(t *testing.T, m *stepModel, tools ...tool.Tool) (*Runtime, types.Agent, *session.Manager) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	router := provider.NewRouter()
	router.Register("step", m)
	router.Register("other", &captureCompleter{})
	reg := tool.New(nil)
	for _, tl := range tools {
		reg.Register(tl)
	}
	sess := session.New(s, bus)
	rt := New(s, bus, sess, nil, router, reg)
	ag, _ := rt.Upsert(context.Background(), types.Agent{Name: "a", Provider: "step", Model: "m"})
	return rt, ag, sess
}

func TestRunRequestToolPolicyNarrowsTools(t *testing.T) {
	m := &stepModel{steps: [][]types.ToolCall{{{ID: "1", Name: "shell", ArgsJSON: `{"command":"echo hi"}`}}}}
	rt, ag, sess := policyRig(t, m, tool.ReadFile{}, tool.Shell{})
	ctx := context.Background()
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "x", AllowedTools: []string{"read_file"}, Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if tools := m.reqs[0].Tools; len(tools) != 1 || tools[0].Name != "read_file" {
		t.Fatalf("offered = %+v", tools)
	}
	// a call outside the policy is refused, not run
	msgs, _ := sess.History(ctx, s.ID)
	refused := false
	for _, msg := range msgs {
		if msg.ToolResult != nil && msg.ToolResult.Name == "shell" && msg.ToolResult.IsError {
			refused = true
		}
	}
	if !refused {
		t.Fatal("shell ran despite the run's tool policy")
	}
}

func TestReadOnlyToolsRunInParallelWhenAsked(t *testing.T) {
	var now, peak int32
	calls := []types.ToolCall{
		{ID: "a", Name: "read_file", ArgsJSON: `{"path":"a"}`},
		{ID: "b", Name: "read_file", ArgsJSON: `{"path":"b"}`},
		{ID: "c", Name: "read_file", ArgsJSON: `{"path":"c"}`},
	}
	for _, conc := range []int{1, 3} {
		now, peak = 0, 0
		m := &stepModel{steps: [][]types.ToolCall{calls}}
		rt, ag, sess := policyRig(t, m, slowRead{&now, &peak})
		ctx := context.Background()
		s, _ := sess.Create(ctx, "s", ag.ID, "")
		if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "x", ToolConcurrency: conc}); err != nil {
			t.Fatal(err)
		}
		want := int32(1)
		if conc > 1 {
			want = 3
		}
		if peak != want {
			t.Fatalf("concurrency %d: peak %d", conc, peak)
		}
		// results are recorded in call order either way
		msgs, _ := sess.History(ctx, s.ID)
		var order []string
		for _, msg := range msgs {
			if msg.ToolResult != nil {
				order = append(order, msg.ToolResult.ToolCallID)
			}
		}
		if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
			t.Fatalf("order = %v", order)
		}
	}
}

func TestRunRequestModelOverrideAndHistoryTurns(t *testing.T) {
	m := &stepModel{}
	rt, ag, sess := policyRig(t, m)
	ctx := context.Background()
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	for i := 0; i < 4; i++ {
		_, _ = sess.Append(ctx, types.Message{SessionID: s.ID, Role: types.RoleUser, Content: "old"})
		_, _ = sess.Append(ctx, types.Message{SessionID: s.ID, Role: types.RoleAssistant, Content: "reply"})
	}
	if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "new", HistoryTurns: 2}); err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, msg := range m.reqs[0].Messages {
		if msg.Role == "user" {
			users++
		}
	}
	if users != 2 {
		t.Fatalf("sent %d user turns, want 2", users)
	}
	// Model/Provider on the request win over the agent's
	other := &captureCompleter{}
	rt.router.Register("other", other)
	if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "x", Provider: "other", Model: "big"}); err != nil {
		t.Fatal(err)
	}
	if len(other.reqs) != 1 || other.reqs[0].Model != "big" {
		t.Fatalf("override not used: %+v", other.reqs)
	}
}

func TestLastTurnsKeepsARunsOwnToolTurns(t *testing.T) {
	h := []types.Message{
		{Role: types.RoleUser, Content: "1"}, {Role: types.RoleAssistant},
		{Role: types.RoleUser, Content: "2"}, {Role: types.RoleAssistant}, {Role: types.RoleTool}, {Role: types.RoleAssistant}, {Role: types.RoleTool},
	}
	got := lastTurns(h, 1)
	if len(got) != 5 || got[0].Content != "2" {
		t.Fatalf("lastTurns = %+v", got)
	}
	if len(lastTurns(h, 0)) != len(h) || len(lastTurns(h, 9)) != len(h) {
		t.Fatal("0 or more turns than exist must keep everything")
	}
}
