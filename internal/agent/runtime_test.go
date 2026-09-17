package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aether-dev/aether/internal/eventbus"
	"github.com/aether-dev/aether/internal/memory"
	"github.com/aether-dev/aether/internal/provider"
	"github.com/aether-dev/aether/internal/session"
	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/tool"
	"github.com/aether-dev/aether/internal/types"
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
