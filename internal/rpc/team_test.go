package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/team"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func TestSessionTitleFrom(t *testing.T) {
	cases := map[string]string{
		"login sayfasına google ile giriş ekle":                                          "Login sayfasına google ile giriş ekle",
		"<codebase-context>\n### a.go\nfunc x()\n</codebase-context>\n\nlogoyu değiştir": "Logoyu değiştir",
		"şu hatayı düzelt ```go\npanic(1)\n``` lütfen":                                   "Şu hatayı düzelt lütfen",
		"/review": "",
		"https://x.dev bak buna. sonra da testleri çalıştır": "Bak buna. sonra da testleri çalıştır",
	}
	for in, want := range cases {
		if got := sessionTitleFrom(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	long := sessionTitleFrom(strings.Repeat("çokuzun kelime ", 20))
	if r := []rune(long); len(r) > 49 || !strings.HasSuffix(long, "…") || strings.HasSuffix(long, " …") {
		t.Errorf("long = %q", long)
	}
	for _, p := range []string{"", "New chat", "Yeni oturum", "yeni sohbet"} {
		if !placeholderTitle(p) {
			t.Errorf("%q should be a placeholder", p)
		}
	}
}

// A chat starts with at most one Office agent, and every chat — never a
// subagent's channel — may hand work to subagents: delegation stays one
// level deep, as in Hermes' default.
func TestChatAgentAndDelegateVisibility(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	call := func(method string, params any, out any) error {
		t.Helper()
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
	var fe, be types.AgentProfile
	_ = call(protocol.MethodProfileUpsert, map[string]any{"name": "Frontend", "role": "frontend"}, &fe)
	_ = call(protocol.MethodProfileUpsert, map[string]any{"name": "Backend", "role": "backend"}, &be)

	var sess types.Session
	if err := call(protocol.MethodSessionCreate, map[string]any{"space": "office", "profileIds": []types.ID{fe.ID}}, &sess); err != nil {
		t.Fatal(err)
	}
	if sp, err := app.Store.GetSessionPersona(ctx, sess.ID); err != nil || sp.ProfileID != fe.ID {
		t.Fatalf("persona = %+v, %v", sp, err)
	}
	// a chat has one agent, not a cast
	if err := call(protocol.MethodSessionCreate, map[string]any{"profileIds": []types.ID{fe.ID, be.ID}}, nil); err == nil {
		t.Fatal("a chat was started with two agents")
	}
	var list []types.Session
	_ = call(protocol.MethodSessionList, map[string]any{}, &list)
	if len(list) != 1 {
		t.Fatalf("sessions = %+v", list)
	}

	allowed := func(sid types.ID) bool {
		for _, spec := range app.Tools.Specs() {
			if spec.Name == team.DelegateName {
				return app.Agents.PersonaFor(ctx, sid).Allows(spec.Name)
			}
		}
		t.Fatal("delegate tool not registered")
		return false
	}
	sub, err := app.Sess.CreateChildIn(ctx, sess, types.SpaceWorker, "Subagent · x")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed(sess.ID) || allowed(sub.ID) {
		t.Fatal("delegate must be on for the chat and off for its subagents")
	}
}

// scripted is a model that plays Orchestra: as the lead it hands the task
// out with team_delegate and then merges; as a subagent it reports back.
type scripted struct {
	inflight, peak      int32
	leadTools, memTools [][]string
	mu                  sync.Mutex
}

func (*scripted) Kind() types.ProviderKind { return types.ProviderFake }
func (*scripted) Name() string             { return "script" }
func (s *scripted) Complete(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	var tools []string
	for _, t := range req.Tools {
		tools = append(tools, t.Name)
	}
	sys, last := req.Messages[0].Content, req.Messages[len(req.Messages)-1]
	lead := !strings.Contains(sys, "You are a subagent")
	s.mu.Lock()
	if lead {
		s.leadTools = append(s.leadTools, tools)
	} else {
		s.memTools = append(s.memTools, tools)
	}
	s.mu.Unlock()
	ch := make(chan provider.ChatDelta, 1)
	switch {
	case lead && last.Role == "user":
		ch <- provider.ChatDelta{Done: true, ToolCalls: []types.ToolCall{{ID: "c1", Name: team.DelegateName,
			ArgsJSON: `{"tasks":[{"goal":"build the login form"},{"goal":"add POST /login"}]}`}}}
	case lead:
		ch <- provider.ChatDelta{Done: true, Content: "merged:\n" + last.Content}
	default:
		n := atomic.AddInt32(&s.inflight, 1)
		for p := atomic.LoadInt32(&s.peak); n > p && !atomic.CompareAndSwapInt32(&s.peak, p, n); p = atomic.LoadInt32(&s.peak) {
		}
		time.Sleep(80 * time.Millisecond)
		atomic.AddInt32(&s.inflight, -1)
		ch <- provider.ChatDelta{Done: true, Content: "done: " + strings.TrimPrefix(last.Content, "Task from the lead:\n")}
	}
	close(ch)
	return ch, nil
}

// The whole path through the real runtime: the chat's model gets the
// delegate tool, plain subagents run in parallel in their own channels with
// only their task, and the lead gets their reports back to merge.
func TestDelegationEndToEnd(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	sc := &scripted{}
	app.Router.Register("script", sc)
	ag, err := app.Agents.Upsert(ctx, types.Agent{Name: "scripted", Provider: "script", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
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
	var sess types.Session
	call(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &sess)

	var res struct{ Assistant string }
	call(protocol.MethodSessionSend, map[string]any{"sessionId": sess.ID, "content": "Add Google login"}, &res)
	if !strings.HasPrefix(res.Assistant, "merged:") || !strings.Contains(res.Assistant, "TASK 1/2 · subagent · completed · 1 turns\ndone: build the login form") ||
		!strings.Contains(res.Assistant, "TASK 2/2 · subagent · completed · 1 turns\ndone: add POST /login") {
		t.Fatalf("lead's answer = %q", res.Assistant)
	}
	if sc.peak < 2 {
		t.Fatalf("subagents ran one after another (peak %d)", sc.peak)
	}
	has := func(list []string, name string) bool {
		for _, n := range list {
			if n == name {
				return true
			}
		}
		return false
	}
	for _, tools := range sc.leadTools {
		if !has(tools, team.DelegateName) {
			t.Fatal("lead was not offered team_delegate")
		}
	}
	if len(sc.memTools) != 2 {
		t.Fatalf("subagent runs = %d", len(sc.memTools))
	}
	for _, tools := range sc.memTools {
		if has(tools, team.DelegateName) {
			t.Fatal("a subagent was offered team_delegate")
		}
	}

	// the app sees the same work: both tasks done, in order
	var tasks []team.Task
	call(protocol.MethodSubagentList, map[string]any{"sessionId": sess.ID}, &tasks)
	if len(tasks) != 2 || tasks[0].Status != team.StatusCompleted || tasks[1].Status != team.StatusCompleted {
		t.Fatalf("roster = %+v", tasks)
	}
	// each subagent's channel holds its own task and answer, nothing else
	for i, want := range []string{"build the login form", "add POST /login"} {
		var hist []types.Message
		call(protocol.MethodSessionHistory, map[string]any{"sessionId": tasks[i].SessionID}, &hist)
		if len(hist) != 2 || !strings.HasSuffix(hist[0].Content, want) || hist[1].Content != "done: "+want {
			t.Fatalf("task %d channel = %+v", i+1, hist)
		}
		if sp, err := app.Store.GetSessionPersona(ctx, tasks[i].SessionID); err == nil && (sp.CharacterID != "" || sp.ProfileID != "") {
			t.Fatalf("a subagent got a persona: %+v", sp)
		}
	}
	// a subagent's channel finds its chat's roster too
	var fromChannel []team.Task
	call(protocol.MethodSubagentList, map[string]any{"sessionId": tasks[0].SessionID}, &fromChannel)
	if len(fromChannel) != 2 {
		t.Fatalf("roster from a channel = %+v", fromChannel)
	}
	// stopping a finished one changes nothing and is not an error
	var stopped team.Task
	call(protocol.MethodSubagentStop, map[string]any{"id": tasks[0].ID}, &stopped)
	if stopped.Status != team.StatusCompleted {
		t.Fatalf("stop rewrote a finished task: %+v", stopped)
	}
}

// Office agents are catalog characters: a chat starts with one and is
// badged by it.
func TestCharacterChats(t *testing.T) {
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
	var sess types.Session
	if err := call(protocol.MethodSessionCreate, map[string]any{"characterId": "marketing"}, &sess); err != nil {
		t.Fatal(err)
	}
	var badges struct {
		Sessions map[string]struct {
			Name        string `json:"name"`
			CharacterID string `json:"characterId"`
		} `json:"sessions"`
	}
	if err := call(protocol.MethodPersonaBadges, map[string]any{}, &badges); err != nil {
		t.Fatal(err)
	}
	if b := badges.Sessions[string(sess.ID)]; b.CharacterID != "marketing" || b.Name != "Reklam & Pazarlama Uzmanı" {
		t.Fatalf("badge = %+v", b)
	}
	if err := call(protocol.MethodSessionCreate, map[string]any{"characterId": "nope"}, nil); err == nil {
		t.Fatal("unknown character accepted")
	}
}

// A chat belongs to Office or Chat from its creation.
func TestSessionSpaces(t *testing.T) {
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
	var office, chat types.Session
	if err := call(protocol.MethodSessionCreate, map[string]any{"space": "office", "characterId": "uiux"}, &office); err != nil {
		t.Fatal(err)
	}
	if err := call(protocol.MethodSessionCreate, map[string]any{"space": "chat"}, &chat); err != nil {
		t.Fatal(err)
	}
	if err := call(protocol.MethodSessionCreate, map[string]any{"space": "garage"}, nil); err == nil {
		t.Fatal("unknown space accepted")
	}
	var list []types.Session
	_ = call(protocol.MethodSessionList, map[string]any{}, &list)
	spaces := map[types.ID]string{}
	for _, x := range list {
		spaces[x.ID] = x.Space
	}
	if spaces[office.ID] != types.SpaceOffice || spaces[chat.ID] != types.SpaceChat {
		t.Fatalf("spaces = %v", spaces)
	}
}
