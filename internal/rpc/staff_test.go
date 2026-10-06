package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/staff"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// An agent stands on the Session Map like a chat does, a watch ties a chat
// to it, and removing the agent removes its watches.
func TestAgentsOnTheSessionMap(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	call := func(method string, params, out any) error {
		raw, _ := json.Marshal(params)
		resp := s.Dispatch(ctx, protocol.Request{Method: method, Token: app.Token, Params: raw})
		if !resp.OK {
			return errors.New(resp.Error)
		}
		if out != nil {
			_ = json.Unmarshal(resp.Result, out)
		}
		return nil
	}
	var qa types.AgentProfile
	if err := call(protocol.MethodProfileUpsert, map[string]any{"name": "Ayşe", "title": "Test uzmanı"}, &qa); err != nil {
		t.Fatal(err)
	}
	if qa.Title != "Test uzmanı" {
		t.Fatalf("title not kept: %+v", qa)
	}
	var chat types.Session
	_ = call(protocol.MethodSessionCreate, map[string]any{"space": "chat"}, &chat)

	if err := call(protocol.MethodCtxPlace, map[string]any{"sessionId": "agent:" + string(qa.ID), "x": 10, "y": 20}, nil); err != nil {
		t.Fatalf("placing an agent: %v", err)
	}
	if err := call(protocol.MethodCtxPlace, map[string]any{"sessionId": "agent:nobody"}, nil); err == nil {
		t.Fatal("an unknown agent was placed")
	}
	var g struct{ Nodes []types.MapNode }
	_ = call(protocol.MethodCtxGet, nil, &g)
	if len(g.Nodes) != 1 || g.Nodes[0].SessionID != "agent:"+qa.ID {
		t.Fatalf("nodes = %+v", g.Nodes)
	}

	var w staff.Watch
	if err := call(protocol.MethodStaffWatchSave, map[string]any{"sessionId": chat.ID, "profileId": qa.ID}, &w); err != nil || !w.Enabled {
		t.Fatalf("watch = %+v, %v", w, err)
	}
	var list []staff.Watch
	_ = call(protocol.MethodStaffWatches, nil, &list)
	if len(list) != 1 {
		t.Fatalf("watches = %+v", list)
	}
	// a cable takes a colour from the palette; anything else is ignored
	var other types.Session
	_ = call(protocol.MethodSessionCreate, map[string]any{"space": "chat"}, &other)
	var l types.SessionLink
	if err := call(protocol.MethodCtxLink, map[string]any{"sessionA": chat.ID, "sessionB": other.ID}, &l); err != nil {
		t.Fatal(err)
	}
	_ = call(protocol.MethodCtxUpdateLink, map[string]any{"id": l.ID, "color": "violet"}, &l)
	_ = call(protocol.MethodCtxUpdateLink, map[string]any{"id": l.ID, "color": "#ff00ff"}, &l)
	var g2 struct{ Links []types.SessionLink }
	_ = call(protocol.MethodCtxGet, nil, &g2)
	if len(g2.Links) != 1 || g2.Links[0].Color != "violet" {
		t.Fatalf("links = %+v", g2.Links)
	}
	w.Color = "aqua"
	_ = call(protocol.MethodStaffWatchSave, w, &w)
	if w.Color != "aqua" {
		t.Fatalf("watch colour = %q", w.Color)
	}

	// the agent leaves: its watches go too
	_ = call(protocol.MethodProfileDelete, map[string]any{"id": qa.ID}, nil)
	_ = call(protocol.MethodStaffWatches, nil, &list)
	if len(list) != 0 {
		t.Fatalf("watches left behind: %+v", list)
	}
}

// The automation tools reach a chat the user talks in, and nothing else.
func TestAutomationToolsReachUserChatsOnly(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	ctx := context.Background()
	allowed := func(sid types.ID, name string) bool {
		return app.Agents.PersonaFor(ctx, sid).Allows(name)
	}
	registered := map[string]bool{}
	for _, spec := range app.Tools.Specs() {
		registered[spec.Name] = true
	}
	for _, n := range []string{staff.AutoListName, staff.AutoWatchName, staff.AutoLinkName, staff.AutoScheduleName, staff.AutoMonitorName, staff.AutoHandoffName, staff.AutoRemoveName} {
		if !registered[n] {
			t.Fatalf("%s is not registered", n)
		}
	}
	chat, _ := app.Sess.CreateIn(ctx, types.SpaceChat, "c", "", "")
	sub, _ := app.Sess.CreateChildIn(ctx, chat, types.SpaceWorker, "Subagent")
	pane, _ := app.Sess.CreateChildIn(ctx, chat, types.SpaceTerminal, "T2")
	if !allowed(chat.ID, staff.AutoWatchName) || !allowed(chat.ID, staff.AutoScheduleName) {
		t.Fatal("a Code chat cannot set up automations")
	}
	if allowed(sub.ID, staff.AutoWatchName) || allowed(pane.ID, staff.AutoScheduleName) {
		t.Fatal("a channel can set up automations")
	}
}

// asker is a model that, asked in a chat, sets up a watch with the tool and
// then says so.
type asker struct{ tools []string }

func (*asker) Kind() types.ProviderKind { return types.ProviderFake }
func (*asker) Name() string             { return "asker" }
func (a *asker) Complete(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	ch := make(chan provider.ChatDelta, 1)
	last := req.Messages[len(req.Messages)-1]
	if last.Role == "user" {
		for _, t := range req.Tools {
			a.tools = append(a.tools, t.Name)
		}
		ch <- provider.ChatDelta{Done: true, ToolCalls: []types.ToolCall{{ID: "c1", Name: staff.AutoWatchName,
			ArgsJSON: `{"agent":"Ayşe","instruction":"Her değişiklikte testleri gözden geçir","filter":"*.go"}`}}}
	} else {
		ch <- provider.ChatDelta{Done: true, Content: "Kurdum: " + last.Content}
	}
	close(ch)
	return ch, nil
}

// The whole way from a Code chat: the user asks in plain words, the model
// calls the tool, and the watch is there — on the map too.
func TestAChatSetsUpAnAutomationWhenAsked(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	m := &asker{}
	app.Router.Register("asker", m)
	ag, _ := app.Agents.Upsert(ctx, types.Agent{Name: "a", Provider: "asker", Model: "m"})
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
	var qa types.AgentProfile
	call(protocol.MethodProfileUpsert, map[string]any{"name": "Ayşe", "title": "Test uzmanı"}, &qa)
	var chat types.Session
	call(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &chat)
	var res struct{ Assistant string }
	call(protocol.MethodSessionSend, map[string]any{"sessionId": chat.ID, "content": "Ayşe bu sohbeti izlesin, Go dosyaları değişince testlere baksın"}, &res)
	if !strings.Contains(res.Assistant, "Ayşe watches") {
		t.Fatalf("reply = %q", res.Assistant)
	}
	var ws []staff.Watch
	call(protocol.MethodStaffWatches, nil, &ws)
	if len(ws) != 1 || ws[0].SessionID != chat.ID || ws[0].ProfileID != qa.ID || ws[0].Filter != "*.go" {
		t.Fatalf("watches = %+v", ws)
	}
	var g struct{ Nodes []types.MapNode }
	call(protocol.MethodCtxGet, nil, &g)
	if len(g.Nodes) != 2 {
		t.Fatalf("map = %+v", g.Nodes)
	}
}

type fakeTool struct{ name string }

func (f fakeTool) Name() string                             { return f.name }
func (fakeTool) Description() string                        { return "" }
func (fakeTool) Parameters() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (fakeTool) RequiredPermission() types.PermissionAction { return types.PermNetwork }
func (fakeTool) Call(context.Context, tool.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}

// An agent given integrations sees only those MCP servers' tools; one given
// none picked keeps them all.
func TestAgentIntegrations(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	app.Tools.Register(fakeTool{"mcp_github_list_prs"})
	app.Tools.Register(fakeTool{"mcp_sentry_issues"})
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
	var ops, free types.AgentProfile
	call(protocol.MethodProfileUpsert, map[string]any{"name": "Ali", "integrations": []string{"github"}}, &ops)
	call(protocol.MethodProfileUpsert, map[string]any{"name": "Ayşe"}, &free)
	if len(ops.Integrations) != 1 || free.Integrations != nil {
		t.Fatalf("stored = %+v / %+v", ops.Integrations, free.Integrations)
	}
	var a, b types.Session
	call(protocol.MethodSessionCreate, map[string]any{"space": "office", "profileIds": []types.ID{ops.ID}}, &a)
	call(protocol.MethodSessionCreate, map[string]any{"space": "office", "profileIds": []types.ID{free.ID}}, &b)
	allows := func(sid types.ID, name string) bool { return app.Agents.PersonaFor(ctx, sid).Allows(name) }
	if !allows(a.ID, "mcp_github_list_prs") || allows(a.ID, "mcp_sentry_issues") {
		t.Fatal("Ali should have GitHub only")
	}
	if !allows(b.ID, "mcp_github_list_prs") || !allows(b.ID, "mcp_sentry_issues") {
		t.Fatal("Ayşe, with none picked, should have both")
	}
}
