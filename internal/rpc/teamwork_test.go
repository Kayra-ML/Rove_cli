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
	"github.com/Kayra-ML/rove/internal/team"
	"github.com/Kayra-ML/rove/internal/teamwork"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// planModel plans as the Teamwork planner and reports as a phase agent.
type planModel struct {
	mu      sync.Mutex
	planned string // the planner's user message
	tools   [][]string
}

func (*planModel) Kind() types.ProviderKind { return types.ProviderFake }
func (*planModel) Name() string             { return "plan" }
func (m *planModel) Complete(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	ch := make(chan provider.ChatDelta, 1)
	sys := req.Messages[0].Content
	m.mu.Lock()
	switch {
	case strings.Contains(sys, "You plan work for several small orchestras"):
		m.planned = req.Messages[1].Content
		ch <- provider.ChatDelta{Done: true, Content: "```json\n" + `{"summary":"Google login","phases":[
			{"id":"a","title":"Login API","files":["server/auth"],"agents":[{"character":"go-backend"},{"character":"database","why":"the users table"},{"character":"security"}]},
			{"id":"b","title":"Login button","files":["web/login"],"agents":[{"character":"frontend"}]}]}` + "\n```"}
	default:
		var names []string
		for _, t := range req.Tools {
			names = append(names, t.Name)
		}
		m.tools = append(m.tools, names)
		ch <- provider.ChatDelta{Done: true, Content: "report: " + req.Messages[len(req.Messages)-1].Content[:20]}
	}
	m.mu.Unlock()
	close(ch)
	return ch, nil
}

func TestTeamworkThroughRPC(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	m := &planModel{}
	app.Router.Register("plan", m)
	ag, _ := app.Agents.Upsert(ctx, types.Agent{Name: "p", Provider: "plan", Model: "m"})
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
	var chat types.Session
	call(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &chat)

	var none json.RawMessage
	call(protocol.MethodWorkGet, map[string]any{"sessionId": chat.ID}, &none)
	if string(none) != "null" {
		t.Fatalf("plan before planning: %s", none)
	}

	var p teamwork.Plan
	call(protocol.MethodWorkPlan, map[string]any{"sessionId": chat.ID, "prompt": "Google ile giriş ekle"}, &p)
	if p.Status != teamwork.StatusDraft || len(p.Phases) != 3 || p.Agents() != 4 {
		t.Fatalf("draft = %+v", p)
	}
	if !strings.Contains(m.planned, "Google ile giriş ekle") {
		t.Fatalf("planner request = %q", m.planned)
	}
	if got, _ := app.Sess.Get(ctx, chat.ID); got.Title != "Google ile giriş ekle" {
		t.Fatalf("chat title = %q", got.Title)
	}
	call(protocol.MethodWorkApprove, map[string]any{"planId": p.ID}, &p)
	if !app.Work.Wait(p.ID, 5*time.Second) {
		t.Fatal("plan did not finish")
	}
	call(protocol.MethodWorkGet, map[string]any{"sessionId": chat.ID}, &p)
	if p.Status != teamwork.StatusDone || !strings.HasPrefix(p.Phases[2].Report, "report: You merge") {
		t.Fatalf("done plan = %+v", p)
	}

	// part channels are not chats of their own
	var list []types.Session
	call(protocol.MethodSessionList, map[string]any{}, &list)
	if len(list) != 1 {
		t.Fatalf("channels leaked into the chat list: %+v", list)
	}
	// the engine hands members their pieces: no one is offered the tool
	for _, tools := range m.tools {
		for _, n := range tools {
			if n == team.DelegateName {
				t.Fatal("a part's player was offered team_delegate")
			}
		}
	}
	conductor, member := p.Phases[0].Agents[0].ChannelID, p.Phases[0].Agents[1].ChannelID
	if ms, _ := app.Store.ListChildSessions(ctx, conductor); len(ms) != 1 || ms[0].ID != member {
		t.Fatalf("orchestra members = %+v", ms)
	}

	// deleting the chat takes its plan and channels with it
	call(protocol.MethodSessionDelete, map[string]any{"id": chat.ID}, nil)
	if kids, _ := app.Store.ListChildSessions(ctx, chat.ID); len(kids) != 0 {
		t.Fatalf("orphan channels: %+v", kids)
	}
	if _, err := app.Store.GetSession(ctx, member); err == nil {
		t.Fatal("orphan orchestra member")
	}
	if _, err := app.Store.LatestTeamworkPlan(ctx, chat.ID); err == nil {
		t.Fatal("orphan plan")
	}
}
