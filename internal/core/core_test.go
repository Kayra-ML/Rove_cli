package core

import (
	"context"
	"testing"

	"github.com/aether-dev/aether/internal/config"
	"github.com/aether-dev/aether/internal/types"
)

func TestOpenSeedsAndRecoversRunningCards(t *testing.T) {
	dir := t.TempDir()
	app, err := Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	card, err := app.Kanban.Create(ctx, types.Card{Title: "mid-flight", Column: types.ColRunning})
	if err != nil {
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
	got, err := app2.Kanban.Get(ctx, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Column != types.ColReady {
		t.Fatalf("expected recovered card in ready, got %s", got.Column)
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
