package automation

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/goal"
	"github.com/Kayra-ML/rove/internal/judge"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

func harness(t *testing.T) (*Engine, context.Context) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	bus := eventbus.New()
	t.Cleanup(func() { bus.Close() })
	return New(s, bus, nil), context.Background()
}

func TestUpsertListDelete(t *testing.T) {
	e, ctx := harness(t)
	j, err := e.Upsert(ctx, types.AutomationJob{Name: "sweep", Kind: types.AutoDriveGoals, EverySeconds: 15, Enabled: true})
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
	e, ctx := harness(t)
	_, err := e.Upsert(ctx, types.AutomationJob{Name: "off", Kind: types.AutoDriveGoals, EverySeconds: 1, Enabled: false})
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

// Only goal driving has a runner; a job of another kind (a template that
// has none yet) runs as a no-op instead of doing something unrelated.
func TestJobWithoutRunnerIsANoop(t *testing.T) {
	e, ctx := harness(t)
	if _, err := e.Upsert(ctx, types.AutomationJob{Name: "daily-digest", Kind: "daily_digest", EverySeconds: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	out, err := e.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].LastResult != "noop" {
		t.Fatalf("%+v", out)
	}
}

func TestTickRespectsInterval(t *testing.T) {
	e, ctx := harness(t)
	j, err := e.Upsert(ctx, types.AutomationJob{Name: "slow", Kind: types.AutoDriveGoals, EverySeconds: 3600, Enabled: true})
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
	e, ctx := harness(t)
	j, err := e.Upsert(ctx, types.AutomationJob{Name: "d"})
	if err != nil {
		t.Fatal(err)
	}
	if j.EverySeconds != 30 || j.Kind != types.AutoDriveGoals {
		t.Fatalf("%+v", j)
	}
}

// blocking answers once released; until then a goal driven with it runs.
type blocking struct{ release chan struct{} }

func (blocking) Kind() types.ProviderKind { return types.ProviderFake }
func (blocking) Name() string             { return "blocking" }
func (b blocking) Complete(ctx context.Context, _ provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ch := make(chan provider.ChatDelta, 2)
	ch <- provider.ChatDelta{Content: "done"}
	ch <- provider.ChatDelta{Done: true}
	close(ch)
	return ch, nil
}

// A tick starts pending goals and returns; it does not hold the loop (and
// every other job) for as long as a goal runs.
func TestDriveGoalsDoesNotHoldTheTick(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	ctx := context.Background()
	m := blocking{release: make(chan struct{})}
	r := provider.NewRouter()
	r.Register("blocking", m)
	sess := session.New(s, bus)
	rt := agent.New(s, bus, sess, nil, r, tool.New(nil))
	ag, _ := rt.Upsert(ctx, types.Agent{Name: "a", Provider: "blocking", Model: "m"})
	goals := goal.New(s, bus, rt, judge.New(nil), nil)
	t.Cleanup(goals.Close)
	g, _ := goals.Create(ctx, types.Goal{Title: "slow", AgentID: ag.ID, CompletionContract: types.CompletionContract{Criteria: []string{"x"}}})

	e := New(s, bus, goals)
	if _, err := e.Upsert(ctx, types.AutomationJob{Name: "drive", Kind: types.AutoDriveGoals, EverySeconds: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	done := make(chan []types.AutomationJob, 1)
	go func() { out, _ := e.Tick(ctx); done <- out }()
	select {
	case out := <-done:
		if len(out) != 1 || out[0].LastResult != "started 1" {
			t.Fatalf("tick = %+v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the tick waited for the goal to finish")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !goals.Running(g.ID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !goals.Running(g.ID) {
		t.Fatal("the goal was not started")
	}
	close(m.release)
}
