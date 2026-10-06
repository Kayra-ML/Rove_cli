package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/teamwork"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// plannerSpy answers the Teamwork planner and notes which provider and
// model each plan was asked of.
type plannerSpy struct {
	name string
	mu   *sync.Mutex
	asks *[]string // "provider/model"
}

func (*plannerSpy) Kind() types.ProviderKind { return types.ProviderFake }
func (p *plannerSpy) Name() string           { return p.name }
func (p *plannerSpy) Complete(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	if strings.Contains(req.Messages[0].Content, "You plan work for several small orchestras") {
		p.mu.Lock()
		*p.asks = append(*p.asks, p.name+"/"+req.Model)
		p.mu.Unlock()
	}
	ch := make(chan provider.ChatDelta, 1)
	ch <- provider.ChatDelta{Done: true, Content: "```json\n" + `{"summary":"s","phases":[{"id":"a","title":"A","files":["a"],"agents":[{"character":"frontend"}]}]}` + "\n```"}
	close(ch)
	return ch, nil
}

func TestTeamworkPlannerModel(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	var mu sync.Mutex
	var asks []string
	app.Router.Register("home", &plannerSpy{name: "home", mu: &mu, asks: &asks})
	app.Router.Register("big", &plannerSpy{name: "big", mu: &mu, asks: &asks})
	for _, pr := range []types.Provider{{ID: "p1", Name: "home", Kind: types.ProviderFake, Models: []string{"small"}}, {ID: "p2", Name: "big", Kind: types.ProviderFake, Models: []string{"chat-pick", "planner-x"}}} {
		if err := app.Store.UpsertProvider(ctx, pr); err != nil {
			t.Fatal(err)
		}
	}
	ag, _ := app.Agents.Upsert(ctx, types.Agent{Name: "p", Provider: "home", Model: "small"})
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
	last := func() string {
		mu.Lock()
		defer mu.Unlock()
		return asks[len(asks)-1]
	}
	var chat types.Session
	call(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &chat)

	// no choice: the agent's model plans, and the plan says so
	var p teamwork.Plan
	call(protocol.MethodWorkPlan, map[string]any{"sessionId": chat.ID, "prompt": "bir iş"}, &p)
	if last() != "home/small" || p.Planner == nil || p.Planner.Model != "small" {
		t.Fatalf("default planner: asked %s, plan says %+v", last(), p.Planner)
	}
	// the chat's model (/models) is the next default
	call(protocol.MethodSessionSetModel, map[string]any{"sessionId": chat.ID, "provider": "big", "model": "chat-pick"}, nil)
	call(protocol.MethodWorkPlan, map[string]any{"sessionId": chat.ID, "prompt": "bir iş"}, &p)
	if last() != "big/chat-pick" {
		t.Fatalf("chat model: asked %s", last())
	}
	// a model picked for the plan wins
	call(protocol.MethodWorkPlan, map[string]any{"sessionId": chat.ID, "prompt": "bir iş", "provider": "big", "model": "planner-x"}, &p)
	if last() != "big/planner-x" || p.Planner == nil || *p.Planner != (types.ModelRef{Provider: "big", Model: "planner-x"}) {
		t.Fatalf("picked planner: asked %s, plan says %+v", last(), p.Planner)
	}
	// a model no provider offers is refused before anything is asked
	raw, _ := json.Marshal(map[string]any{"sessionId": chat.ID, "prompt": "bir iş", "provider": "big", "model": "typo"})
	if resp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodWorkPlan, Token: app.Token, Params: raw}); resp.OK || !strings.Contains(resp.Error, "unknown model") {
		t.Fatalf("unknown planner model: %+v", resp)
	}
	// and it is kept with the plan
	var got teamwork.Plan
	call(protocol.MethodWorkGet, map[string]any{"sessionId": chat.ID}, &got)
	if got.Planner == nil || got.Planner.Model != "planner-x" {
		t.Fatalf("stored planner = %+v", got.Planner)
	}
}
