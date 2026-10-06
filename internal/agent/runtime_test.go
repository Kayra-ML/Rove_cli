package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/memory"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

func TestRunStreamsAndPersists(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bus := eventbus.New()
	defer bus.Close()
	sess := session.New(s, bus)
	mem := memory.New(s)
	r := provider.NewRouter()
	r.Register("fake", &provider.Fake{Responses: []string{"hello from agent"}})
	rt := New(s, bus, sess, mem, r, tool.New(nil))
	ctx := context.Background()
	a, err := rt.Upsert(ctx, types.Agent{Name: "t", Provider: "fake", Model: "fake", Profile: "t"})
	if err != nil {
		t.Fatal(err)
	}
	sessionObj, err := sess.Create(ctx, "s", a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := rt.Run(ctx, RunRequest{AgentID: a.ID, SessionID: sessionObj.ID, UserMessage: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Assistant, "hello from agent") {
		t.Fatalf("%+v", res)
	}
	hist, err := sess.History(ctx, sessionObj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) < 2 {
		t.Fatalf("history %d", len(hist))
	}
}

func TestBuildMessagesIncludesWorkspace(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rt := New(s, nil, nil, nil, provider.NewRouter(), tool.New(nil))
	msgs, err := rt.buildMessages(context.Background(), types.Agent{}, RunRequest{Workspace: "/tmp/demo-app"}, Persona{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 || msgs[0].Role != "system" {
		t.Fatalf("%+v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "Rove Code") {
		t.Fatalf("missing brand: %s", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, "/tmp/demo-app") {
		t.Fatalf("missing workspace: %s", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, "demo-app") {
		t.Fatalf("missing project name: %s", msgs[0].Content)
	}
}

func TestCompactNoticeAndTrimHistory(t *testing.T) {
	long := "[bağlam haritası · otomatik] X değişti:\n- a/logo.svg\n- b/x.ts\n\nDeğişiklik:\n```diff\n" + strings.Repeat("+line\n", 200) + "```\nuygula"
	got := CompactNotice(long)
	if strings.Contains(got, "+line") || !strings.Contains(got, "- a/logo.svg") || len(got) > 200 {
		t.Fatalf("compact = %q", got)
	}
	if CompactNotice("hello") != "hello" {
		t.Fatal("plain messages must pass through")
	}
	hist := []types.Message{
		{Role: types.RoleUser}, {Role: types.RoleAssistant}, {Role: types.RoleTool},
		{Role: types.RoleAssistant}, {Role: types.RoleUser}, {Role: types.RoleAssistant},
	}
	if got := trimHistory(hist, 4); len(got) != 2 || got[0].Role != types.RoleUser {
		t.Fatalf("trim cut mid-turn: %+v", got)
	}
	if got := trimHistory(hist, 0); len(got) != 6 {
		t.Fatal("limit 0 keeps everything")
	}
}

type captureCompleter struct {
	reqs []provider.ChatRequest
}

func (c *captureCompleter) Kind() types.ProviderKind { return types.ProviderFake }
func (c *captureCompleter) Name() string             { return "capture" }
func (c *captureCompleter) Complete(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	c.reqs = append(c.reqs, req)
	ch := make(chan provider.ChatDelta, 1)
	ch <- provider.ChatDelta{Content: "ok", Done: true}
	close(ch)
	return ch, nil
}

func TestPersonaShapesRequest(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cap := &captureCompleter{}
	router := provider.NewRouter()
	router.Register("capture", cap)
	tools := tool.New(nil)
	tools.Register(tool.ReadFile{})
	tools.Register(tool.Shell{})
	rt := New(s, nil, nil, nil, router, tools)
	rt.SetPersonaSource(func(context.Context, types.ID) Persona {
		return Persona{Prompt: "You are a database expert.", Allow: func(n string) bool { return n == "read_file" }}
	})
	ag, _ := rt.Upsert(context.Background(), types.Agent{Name: "a", Provider: "capture", Model: "m"})
	if _, err := rt.Run(context.Background(), RunRequest{AgentID: ag.ID, SessionID: "sx", UserMessage: "hi"}); err != nil {
		t.Fatal(err)
	}
	req := cap.reqs[0]
	if !strings.Contains(req.Messages[0].Content, "database expert") || strings.Contains(req.Messages[0].Content, "You are Rove Code") {
		t.Fatalf("system = %q", req.Messages[0].Content)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "read_file" {
		t.Fatalf("tools = %+v", req.Tools)
	}
	// a character still writes plain working notes
	if !strings.Contains(req.Messages[0].Content, "No emojis") {
		t.Fatalf("no style rule: %q", req.Messages[0].Content)
	}
}

// TestRunPublishesRunDone checks that run.done is its own event, distinct
// from session.Manager's per-append message.done: it must fire even when no
// session manager is wired at all (so nothing was ever appended), which is
// exactly the case an early-failure run needs a completion signal for.
func TestRunPublishesRunDone(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bus := eventbus.New()
	runDone := make(chan types.Event, 4)
	messageDone := make(chan types.Event, 4)
	bus.Subscribe("run.done", func(ev types.Event) { runDone <- ev })
	bus.Subscribe("message.done", func(ev types.Event) { messageDone <- ev })
	router := provider.NewRouter()
	router.Register("capture", &captureCompleter{})
	rt := New(s, bus, nil, nil, router, tool.New(nil)) // no *session.Manager: nothing gets appended
	ag, _ := rt.Upsert(context.Background(), types.Agent{Name: "a", Provider: "capture", Model: "m"})
	if _, err := rt.Run(context.Background(), RunRequest{AgentID: ag.ID, SessionID: "sd", UserMessage: "hi"}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-runDone:
		if ev.Topic != "session.sd" {
			t.Fatalf("topic = %s", ev.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no run.done")
	}
	select {
	case ev := <-messageDone:
		t.Fatalf("message.done fired with no session manager: %+v", ev)
	case <-time.After(100 * time.Millisecond):
		// correct: that event is session.Manager's, not this run's
	}
}
