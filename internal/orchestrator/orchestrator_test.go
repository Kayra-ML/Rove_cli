package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aether-dev/aether/internal/agent"
	"github.com/aether-dev/aether/internal/eventbus"
	"github.com/aether-dev/aether/internal/kanban"
	"github.com/aether-dev/aether/internal/provider"
	"github.com/aether-dev/aether/internal/session"
	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/tool"
	"github.com/aether-dev/aether/internal/types"
)

func TestDispatchMovesToReview(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bus := eventbus.New()
	defer bus.Close()
	k := kanban.New(s, bus)
	r := provider.NewRouter()
	r.Register("fake", &provider.Fake{Responses: []string{"did the work"}})
	sess := session.New(s, bus)
	ag := agent.New(s, bus, sess, nil, r, tool.New(nil))
	o := New(s, bus, k, ag, nil, sess, nil, nil, nil)
	ctx := context.Background()
	a, err := ag.Upsert(ctx, types.Agent{Name: "a", Provider: "fake", Model: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := k.Create(ctx, types.Card{Title: "task", Column: types.ColReady, AssigneeAgentID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	sessionObj, err := sess.Create(ctx, "o", a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Dispatch(ctx, DispatchOpts{CardID: card.ID, SessionID: sessionObj.ID}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := k.Get(ctx, card.ID)
		if got.Column == types.ColReview {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := k.Get(ctx, card.ID)
	t.Fatalf("column %s", got.Column)
}
