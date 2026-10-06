package core

import (
	"context"
	"testing"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/types"
)

func TestOpenSeedsAndRecoversRunningGoals(t *testing.T) {
	dir := t.TempDir()
	app, err := Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	g, err := app.Goals.Create(ctx, types.Goal{Title: "mid-flight"})
	if err != nil {
		t.Fatal(err)
	}
	g.Status = types.GoalRunning
	if err := app.Store.UpsertGoal(ctx, g); err != nil {
		t.Fatal(err)
	}
	// a job of a kind an older version seeded is dropped on the next start
	if _, err := app.Auto.Upsert(ctx, types.AutomationJob{Name: "sweep ready", Kind: "sweep_ready", EverySeconds: 30}); err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	app2, err := Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app2.Close()
	got, err := app2.Goals.Get(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != types.GoalPending {
		t.Fatalf("expected the interrupted goal back to pending, got %s", got.Status)
	}
	if jobs, _ := app2.Auto.List(ctx); len(jobs) != 0 {
		t.Fatalf("old jobs kept: %+v", jobs)
	}
	agents, err := app2.Agents.List(ctx)
	if err != nil || len(agents) == 0 {
		t.Fatalf("expected default agent")
	}
	if app2.Token == "" {
		t.Fatal("expected auth token")
	}
	h := app2.Health()
	if _, ok := h["totalTokens"]; !ok {
		t.Fatalf("health missing tokens: %v", h)
	}
	if _, ok := h["days"]; !ok {
		t.Fatalf("health missing days: %v", h)
	}
	if _, ok := h["activeAgents"]; !ok {
		t.Fatalf("health missing agents: %v", h)
	}
}

func TestApplyProviderHydratesRouterAndRetargetsAgent(t *testing.T) {
	dir := t.TempDir()
	app, err := Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	ctx := context.Background()
	if err := app.Secrets.Put("openai-key", "sk-test"); err != nil {
		t.Fatal(err)
	}
	p, err := app.ApplyProvider(ctx, types.Provider{
		Name:     "openai",
		Kind:     "openai_compat",
		BaseURL:  "https://api.openai.com/v1",
		Models:   []string{"gpt-4o"},
		SecretID: "openai-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != types.ProviderOpenAICompat {
		t.Fatalf("kind = %s", p.Kind)
	}
	c, err := app.Router.Get("openai")
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind() != types.ProviderOpenAICompat {
		t.Fatalf("router kind = %s", c.Kind())
	}
	agents, err := app.Agents.List(ctx)
	if err != nil || len(agents) == 0 {
		t.Fatal(err)
	}
	if agents[0].Provider != "openai" || agents[0].Model != "gpt-4o" {
		t.Fatalf("agent still fake: %+v", agents[0])
	}
	resolved, model, err := app.Router.Resolve("default", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Name() != "openai" {
		t.Fatalf("resolve = %s model=%s", resolved.Name(), model)
	}
}
