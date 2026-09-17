package automation

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aether-dev/aether/internal/agent"
	"github.com/aether-dev/aether/internal/eventbus"
	"github.com/aether-dev/aether/internal/kanban"
	"github.com/aether-dev/aether/internal/orchestrator"
	"github.com/aether-dev/aether/internal/provider"
	"github.com/aether-dev/aether/internal/session"
	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/tool"
	"github.com/aether-dev/aether/internal/types"
)

func harness(t *testing.T) (*Engine, *kanban.Engine, *agent.Runtime, context.Context) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	bus := eventbus.New()
	t.Cleanup(func() { bus.Close() })
	k := kanban.New(s, bus)
	r := provider.NewRouter()
	r.Register("fake", &provider.Fake{Responses: []string{"did the work"}})
	sess := session.New(s, bus)
	ag := agent.New(s, bus, sess, nil, r, tool.New(nil))
	o := orchestrator.New(s, bus, k, ag, nil, sess, nil, nil, nil)
	e := New(s, bus, k, o, nil, ag)
	return e, k, ag, context.Background()
}

func TestUpsertListDelete(t *testing.T) {
	e, _, _, ctx := harness(t)
	j, err := e.Upsert(ctx, types.AutomationJob{Name: "sweep", Kind: types.AutoSweepReady, EverySeconds: 15, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if j.ID == "" || j.EverySeconds != 15 {
		t.Fatalf("%+v", j)
	}
	list, err := e.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, err)
	}
	if err := e.Delete(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	list, err = e.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("after delete %d %v", len(list), err)
	}
}

func TestTickDisabledSkipped(t *testing.T) {
	e, _, _, ctx := harness(t)
	_, err := e.Upsert(ctx, types.AutomationJob{Name: "off", Kind: types.AutoAssignIdle, EverySeconds: 1, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].LastResult != "" {
		t.Fatalf("disabled should not run: %+v", out)
	}
}

func TestAssignIdleFillsReady(t *testing.T) {
	e, k, ag, ctx := harness(t)
	a, err := ag.Upsert(ctx, types.Agent{Name: "lead", Provider: "fake", Model: "fake", Status: types.AgentIdle})
	if err != nil {
		t.Fatal(err)
	}
	card, err := k.Create(ctx, types.Card{Title: "open", Column: types.ColReady})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Upsert(ctx, types.AutomationJob{Name: "assign", Kind: types.AutoAssignIdle, EverySeconds: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].LastResult != "assigned 1" {
		t.Fatalf("%+v", out)
	}
	got, err := k.Get(ctx, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AssigneeAgentID != a.ID {
		t.Fatalf("assignee %s want %s", got.AssigneeAgentID, a.ID)
	}
}

func TestSweepDispatchesReady(t *testing.T) {
	e, k, ag, ctx := harness(t)
	a, err := ag.Upsert(ctx, types.Agent{Name: "lead", Provider: "fake", Model: "fake", Status: types.AgentIdle})
	if err != nil {
		t.Fatal(err)
	}
	card, err := k.Create(ctx, types.Card{Title: "ship", Column: types.ColReady, AssigneeAgentID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Upsert(ctx, types.AutomationJob{Name: "sweep", Kind: types.AutoSweepReady, EverySeconds: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := k.Get(ctx, card.ID)
		if got.Column == types.ColRunning || got.Column == types.ColReview {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := k.Get(ctx, card.ID)
	t.Fatalf("column %s", got.Column)
}

func TestTickRespectsInterval(t *testing.T) {
	e, _, _, ctx := harness(t)
	j, err := e.Upsert(ctx, types.AutomationJob{Name: "slow", Kind: types.AutoAssignIdle, EverySeconds: 3600, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	first, _ := e.List(ctx)
	if len(first) != 1 || first[0].LastRunAt.IsZero() {
		t.Fatalf("expected first run %+v", first)
	}
	j.LastRunAt = first[0].LastRunAt
	if _, err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	second, _ := e.List(ctx)
	if !second[0].LastRunAt.Equal(j.LastRunAt) {
		t.Fatalf("second tick should skip: %v vs %v", second[0].LastRunAt, j.LastRunAt)
	}
}

func TestDefaultEverySeconds(t *testing.T) {
	e, _, _, ctx := harness(t)
	j, err := e.Upsert(ctx, types.AutomationJob{Name: "d"})
	if err != nil {
		t.Fatal(err)
	}
	if j.EverySeconds != 30 || j.Kind != types.AutoSweepReady {
		t.Fatalf("%+v", j)
	}
}
