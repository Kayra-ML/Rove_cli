package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// An office agent is checked when it is saved, and a chat opened with it
// is grouped under it with its logo.
func TestOfficeAgent(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	_ = app.Store.UpsertProvider(ctx, types.Provider{ID: "p1", Name: "home", Kind: types.ProviderFake, Models: []string{"small"}})
	call := func(method string, params any) (json.RawMessage, string) {
		raw, _ := json.Marshal(params)
		resp := s.Dispatch(ctx, protocol.Request{Method: method, Token: app.Token, Params: raw})
		return resp.Result, resp.Error
	}
	for _, bad := range []map[string]any{
		{"name": " "},
		{"name": "A", "provider": "home", "model": "nope"},
		{"name": "A", "characterId": "ghost"},
		{"name": "A", "promptMode": "weird"},
	} {
		if _, e := call(protocol.MethodProfileUpsert, bad); e == "" {
			t.Fatalf("accepted %v", bad)
		}
	}
	raw, e := call(protocol.MethodProfileUpsert, map[string]any{"name": "Ayşe", "characterId": "frontend", "provider": "home", "model": "small", "mark": "drop:dots", "color": "#e5735f"})
	if e != "" {
		t.Fatal(e)
	}
	var p types.AgentProfile
	_ = json.Unmarshal(raw, &p)
	raw, e = call(protocol.MethodSessionCreate, map[string]any{"space": "office", "profileIds": []string{string(p.ID)}})
	if e != "" {
		t.Fatal(e)
	}
	var sess types.Session
	_ = json.Unmarshal(raw, &sess)
	raw, _ = call(protocol.MethodPersonaBadges, nil)
	if !strings.Contains(string(raw), `"profileId":"`+string(p.ID)+`"`) || !strings.Contains(string(raw), `"mark":"drop:dots"`) || !strings.Contains(string(raw), string(sess.ID)) {
		t.Fatalf("badges = %s", raw)
	}
}
