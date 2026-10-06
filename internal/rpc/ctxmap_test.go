package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func TestContextMapCanvasAndCables(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	agents, _ := app.Agents.List(ctx)
	a, _ := app.Sess.Create(ctx, "web", agents[0].ID, "")
	b, _ := app.Sess.Create(ctx, "desktop", agents[0].ID, "")

	call := func(method string, params map[string]any) protocol.Response {
		raw, _ := json.Marshal(params)
		return s.Dispatch(ctx, protocol.Request{Method: method, Token: app.Token, Params: raw})
	}
	must := func(r protocol.Response) json.RawMessage {
		t.Helper()
		if !r.OK {
			t.Fatal(r.Error)
		}
		return r.Result
	}
	must(call(protocol.MethodCtxPlace, map[string]any{"sessionId": a.ID, "x": 10, "y": 20}))
	must(call(protocol.MethodCtxPlace, map[string]any{"sessionId": b.ID, "x": 300, "y": 20}))
	if r := call(protocol.MethodCtxLink, map[string]any{"sessionA": a.ID, "sessionB": a.ID}); r.OK {
		t.Fatal("self cable must be refused")
	}
	var l1, l2 types.SessionLink
	_ = json.Unmarshal(must(call(protocol.MethodCtxLink, map[string]any{"sessionA": a.ID, "sessionB": b.ID})), &l1)
	_ = json.Unmarshal(must(call(protocol.MethodCtxLink, map[string]any{"sessionA": b.ID, "sessionB": a.ID})), &l2)
	if l1.ID == "" || l1.ID != l2.ID || !l1.Auto || l1.Mode != types.LinkSmart || l1.Direction != types.LinkBoth {
		t.Fatalf("cables %+v / %+v", l1, l2)
	}
	var up types.SessionLink
	_ = json.Unmarshal(must(call(protocol.MethodCtxUpdateLink, map[string]any{"id": l1.ID, "direction": "a2b", "auto": false, "label": "logo"})), &up)
	if up.Direction != "a2b" || up.Auto || up.Label != "logo" || up.Mode != types.LinkSmart {
		t.Fatalf("update = %+v", up)
	}

	var g struct {
		Nodes []types.MapNode     `json:"nodes"`
		Links []types.SessionLink `json:"links"`
	}
	_ = json.Unmarshal(must(call(protocol.MethodCtxGet, nil)), &g)
	if len(g.Nodes) != 2 || len(g.Links) != 1 || g.Links[0].Label != "logo" {
		t.Fatalf("get = %+v", g)
	}

	// taking a card off the canvas drops its cables
	must(call(protocol.MethodCtxRemove, map[string]any{"sessionId": a.ID}))
	_ = json.Unmarshal(must(call(protocol.MethodCtxGet, nil)), &g)
	if len(g.Nodes) != 1 || len(g.Links) != 0 {
		t.Fatalf("after remove = %+v", g)
	}
}

// Each chat has one assistant on the map, a hidden child: asked for twice it
// is the same session, and it never shows up in lists or on the team.
func TestContextMapAssistant(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	agents, _ := app.Agents.List(ctx)
	chat, _ := app.Sess.Create(ctx, "Naber", agents[0].ID, "")
	_ = app.Store.SetSessionModel(ctx, chat.ID, types.ModelRef{Provider: "Kira", Model: "claude-sonnet-4-6"})
	call := func(params map[string]any) protocol.Response {
		raw, _ := json.Marshal(params)
		return s.Dispatch(ctx, protocol.Request{Method: protocol.MethodCtxAssistant, Token: app.Token, Params: raw})
	}
	var a1, a2 types.Session
	r := call(map[string]any{"sessionId": chat.ID})
	if !r.OK {
		t.Fatal(r.Error)
	}
	_ = json.Unmarshal(r.Result, &a1)
	_ = json.Unmarshal(call(map[string]any{"sessionId": chat.ID}).Result, &a2)
	if a1.ID == "" || a1.ID != a2.ID || a1.ParentID != chat.ID || a1.Space != types.SpaceMap {
		t.Fatalf("assistant %+v / %+v", a1, a2)
	}
	// it talks to the chat's own model, which is known to work
	if m, err := app.Store.GetSessionModel(ctx, a1.ID); err != nil || m.Model != "claude-sonnet-4-6" || m.Provider != "Kira" {
		t.Fatalf("assistant model = %+v, %v", m, err)
	}
	if list, _ := app.Sess.List(ctx, ""); len(list) != 1 {
		t.Fatalf("the assistant must not be listed: %d sessions", len(list))
	}
	if r := call(map[string]any{"sessionId": a1.ID}); r.OK {
		t.Fatal("an assistant has no assistant of its own")
	}
}
