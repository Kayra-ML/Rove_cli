package rpc

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// modelRecorder answers "ok" and remembers which model each call asked for.
type modelRecorder struct {
	mu     sync.Mutex
	name   string
	models []string
}

func (*modelRecorder) Kind() types.ProviderKind { return types.ProviderFake }
func (r *modelRecorder) Name() string           { return r.name }
func (r *modelRecorder) Complete(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	r.mu.Lock()
	r.models = append(r.models, req.Model)
	r.mu.Unlock()
	ch := make(chan provider.ChatDelta, 2)
	ch <- provider.ChatDelta{Content: "ok"}
	ch <- provider.ChatDelta{Done: true}
	close(ch)
	return ch, nil
}
func (r *modelRecorder) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.models) == 0 {
		return ""
	}
	return r.models[len(r.models)-1]
}

func TestModelsCommandPicksAChatsModel(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	for _, p := range []types.Provider{
		{Name: "alpha", Kind: types.ProviderFake, Models: []string{"a-small", "a-large"}},
		{Name: "beta", Kind: types.ProviderFake, Models: []string{"b-1"}},
	} {
		if _, err := app.ApplyProvider(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	alpha, beta := &modelRecorder{name: "alpha"}, &modelRecorder{name: "beta"}
	app.Router.Register("alpha", alpha)
	app.Router.Register("beta", beta)
	ag, _ := app.Agents.Upsert(ctx, types.Agent{Name: "a", Provider: "alpha", Model: "a-small"})

	call := func(method string, params, out any) error {
		t.Helper()
		raw, _ := json.Marshal(params)
		resp := s.Dispatch(ctx, protocol.Request{Method: method, Token: app.Token, Params: raw})
		if !resp.OK {
			return &remoteErr{resp.Error}
		}
		if out != nil {
			_ = json.Unmarshal(resp.Result, out)
		}
		return nil
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	var list []modelChoice
	must(call(protocol.MethodModelList, nil, &list))
	got := map[string]bool{}
	for _, m := range list {
		got[m.Provider+"/"+m.Model] = true
	}
	for _, want := range []string{"alpha/a-small", "alpha/a-large", "beta/b-1"} {
		if !got[want] {
			t.Fatalf("model.list = %+v, missing %s", list, want)
		}
	}

	var chat, other types.Session
	must(call(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &chat))
	must(call(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &other))
	var cur sessionModel
	must(call(protocol.MethodSessionModel, map[string]any{"sessionId": chat.ID}, &cur))
	if cur != (sessionModel{Provider: "alpha", Model: "a-small", Source: "agent"}) {
		t.Fatalf("before = %+v", cur)
	}

	// a typo fails now, not on the next message
	if err := call(protocol.MethodSessionSetModel, map[string]any{"sessionId": chat.ID, "provider": "beta", "model": "nope"}, nil); err == nil {
		t.Fatal("unknown model accepted")
	}
	must(call(protocol.MethodSessionSetModel, map[string]any{"sessionId": chat.ID, "provider": "beta", "model": "b-1"}, nil))
	must(call(protocol.MethodSessionModel, map[string]any{"sessionId": chat.ID}, &cur))
	if cur != (sessionModel{Provider: "beta", Model: "b-1", Source: "chat"}) {
		t.Fatalf("after = %+v", cur)
	}

	// the chat's next message runs on it — only this chat's
	send := func(sid types.ID) {
		t.Helper()
		must(call(protocol.MethodSessionSend, map[string]any{"sessionId": sid, "agentId": ag.ID, "content": "merhaba"}, nil))
	}
	send(chat.ID)
	if beta.last() != "b-1" {
		t.Fatalf("chat ran on %q/%q", alpha.last(), beta.last())
	}
	send(other.ID)
	if alpha.last() != "a-small" {
		t.Fatalf("other chat ran on %q", alpha.last())
	}

	// clearing goes back to the agent's model; deleting the chat drops it
	must(call(protocol.MethodSessionSetModel, map[string]any{"sessionId": chat.ID, "model": ""}, nil))
	must(call(protocol.MethodSessionModel, map[string]any{"sessionId": chat.ID}, &cur))
	if cur.Source != "agent" || cur.Model != "a-small" {
		t.Fatalf("cleared = %+v", cur)
	}
	must(call(protocol.MethodSessionSetModel, map[string]any{"sessionId": chat.ID, "provider": "alpha", "model": "a-large"}, nil))
	must(call(protocol.MethodSessionDelete, map[string]any{"id": chat.ID}, nil))
	if _, err := app.Store.GetSessionModel(ctx, chat.ID); err == nil {
		t.Fatal("model kept after the chat was deleted")
	}
}

type remoteErr struct{ msg string }

func (e *remoteErr) Error() string { return e.msg }
