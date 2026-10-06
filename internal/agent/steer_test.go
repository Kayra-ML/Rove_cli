package agent

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

// stepper calls a tool on its first turn — waiting there until it is told to
// go on, so a steer can arrive mid-run — and answers on the second.
type stepper struct {
	mu      sync.Mutex
	gate    chan struct{}
	started chan struct{}
	second  []provider.ChatMessage
}

func (*stepper) Kind() types.ProviderKind { return types.ProviderFake }
func (*stepper) Name() string             { return "stepper" }
func (s *stepper) Complete(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	ch := make(chan provider.ChatDelta, 1)
	s.mu.Lock()
	first := s.second == nil && s.gate != nil
	s.mu.Unlock()
	if first {
		close(s.started)
		<-s.gate
		s.mu.Lock()
		s.gate = nil
		s.mu.Unlock()
		ch <- provider.ChatDelta{Done: true, ToolCalls: []types.ToolCall{{ID: "t1", Name: "list_dir", ArgsJSON: `{"path":"."}`}}}
	} else {
		s.mu.Lock()
		s.second = req.Messages
		s.mu.Unlock()
		ch <- provider.ChatDelta{Done: true, Content: "done"}
	}
	close(ch)
	return ch, nil
}

// A run that has gone the wrong way can be turned without being killed: the
// words reach it before its next turn, as the latest thing it was told.
func TestSteerReachesTheNextTurn(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sess := session.New(st, nil)
	model := &stepper{gate: make(chan struct{}), started: make(chan struct{})}
	router := provider.NewRouter()
	router.Register("stepper", model)
	tools := tool.New(nil)
	tools.Register(tool.ListDir{})
	rt := New(st, nil, sess, nil, router, tools)
	ctx := context.Background()
	ag, _ := rt.Upsert(ctx, types.Agent{Name: "a", Provider: "stepper", Model: "m"})
	s, _ := sess.Create(ctx, "s", ag.ID, "")

	if rt.Steer(s.ID, "too early") {
		t.Fatal("a steer was taken with nothing running")
	}
	done := make(chan RunResult)
	go func() {
		res, _ := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "go", Workspace: t.TempDir()})
		done <- res
	}()
	<-model.started
	if !rt.Steer(s.ID, "use the v2 API") {
		t.Fatal("a running session refused a steer")
	}
	close(model.gate)
	if res := <-done; res.Assistant != "done" {
		t.Fatalf("run = %+v", res)
	}
	last := model.second[len(model.second)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "[steer] use the v2 API") {
		t.Fatalf("the steer did not reach the next turn: %+v", last)
	}
	if rt.Steer(s.ID, "after") {
		t.Fatal("a finished run took a steer")
	}
}
