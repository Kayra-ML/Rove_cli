package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// goalModel works on goals and reviews them: a goal titled "yavaş" waits
// until canceled; the reviewer accepts everything else.
type goalModel struct {
	mu    sync.Mutex
	works int
}

func (*goalModel) Kind() types.ProviderKind { return types.ProviderFake }
func (*goalModel) Name() string             { return "goalm" }
func (m *goalModel) Complete(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	ch := make(chan provider.ChatDelta, 2)
	if strings.Contains(req.Messages[0].Content, `{"met": [criterion numbers]`) {
		ch <- provider.ChatDelta{Content: `{"met":[1],"missing":[],"reason":"shown"}`}
	} else {
		m.mu.Lock()
		m.works++
		m.mu.Unlock()
		last := req.Messages[len(req.Messages)-1].Content
		if strings.Contains(last, "Goal: yavaş") {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		ch <- provider.ChatDelta{Content: "yaptım"}
	}
	ch <- provider.ChatDelta{Done: true}
	close(ch)
	return ch, nil
}

func TestChatGoalThroughRPC(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	m := &goalModel{}
	app.Router.Register("goalm", m)
	ag, _ := app.Agents.Upsert(ctx, types.Agent{Name: "g", Provider: "goalm", Model: "m"})
	call := func(method string, params, out any) {
		t.Helper()
		raw, _ := json.Marshal(params)
		resp := s.Dispatch(ctx, protocol.Request{Method: method, Token: app.Token, Params: raw})
		if !resp.OK {
			t.Fatalf("%s: %s", method, resp.Error)
		}
		if out != nil {
			_ = json.Unmarshal(resp.Result, out)
		}
	}
	get := func(id types.ID) types.Goal {
		var g types.Goal
		call(protocol.MethodGoalGet, map[string]any{"id": id}, &g)
		return g
	}
	wait := func(id types.ID, st types.GoalStatus) types.Goal {
		t.Helper()
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			if g := get(id); g.Status == st {
				return g
			}
		}
		t.Fatalf("goal %s never became %s (is %s)", id, st, get(id).Status)
		return types.Goal{}
	}

	var chat types.Session
	call(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &chat)

	// what the chat's /goal sends: the chat, no agent — the chat's is used
	var g types.Goal
	call(protocol.MethodGoalCreate, map[string]any{"title": "README yaz", "sessionId": chat.ID, "completionContract": map[string]any{"criteria": []string{"kurulum"}, "maxIterations": 4}}, &g)
	if g.AgentID != ag.ID || g.SessionID != chat.ID {
		t.Fatalf("created = %+v", g)
	}
	call(protocol.MethodGoalDrive, map[string]any{"id": g.ID, "sessionId": chat.ID}, nil)
	done := wait(g.ID, types.GoalDone)
	if done.Iteration != 1 || m.works == 0 {
		t.Fatalf("done = it %d, agent worked %d times", done.Iteration, m.works)
	}
	var hist []types.Message
	call(protocol.MethodSessionHistory, map[string]any{"sessionId": chat.ID}, &hist)
	if len(hist) == 0 {
		t.Fatal("the goal left nothing in the chat")
	}

	// /stop in the chat stops its goal; it can be resumed later
	var slow types.Goal
	call(protocol.MethodGoalCreate, map[string]any{"title": "yavaş", "sessionId": chat.ID, "completionContract": map[string]any{"criteria": []string{"x"}}}, &slow)
	call(protocol.MethodGoalDrive, map[string]any{"id": slow.ID, "sessionId": chat.ID}, nil)
	wait(slow.ID, types.GoalRunning)
	time.Sleep(50 * time.Millisecond)
	var stopped struct{ Goals int }
	call(protocol.MethodSessionCancel, map[string]any{"sessionId": chat.ID}, &stopped)
	if stopped.Goals != 1 {
		t.Fatalf("session.cancel stopped %d goals", stopped.Goals)
	}
	wait(slow.ID, types.GoalCanceled)

}
