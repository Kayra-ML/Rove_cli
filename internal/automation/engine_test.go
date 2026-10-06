package automation

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/store"
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
