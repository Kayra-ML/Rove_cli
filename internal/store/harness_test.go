package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/aether.db")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ── Goal harness persist round-trip ──────────────────────────────────────────

func TestGoalHarnessRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Upsert a goal first.
	g := types.Goal{
		ID:          "g-harness-1",
		Title:       "test harness goal",
		Description: "desc",
		Status:      types.GoalPending,
		CompletionContract: types.CompletionContract{
			Criteria:      []string{"done"},
			MaxIterations: 3,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.UpsertGoal(ctx, g); err != nil {
		t.Fatalf("UpsertGoal: %v", err)
	}

	profile := map[string]any{
		"goalId":  "g-harness-1",
		"current": map[string]any{"mode": "auto", "context": 9, "execution": 3, "version": 1},
	}
	raw, _ := json.Marshal(profile)

	if err := s.SaveGoalHarness(ctx, types.ID("g-harness-1"), string(raw)); err != nil {
		t.Fatalf("SaveGoalHarness: %v", err)
	}

	got, err := s.LoadGoalHarness(ctx, types.ID("g-harness-1"))
	if err != nil {
		t.Fatalf("LoadGoalHarness: %v", err)
	}
	if got == "" {
		t.Fatal("LoadGoalHarness: returned empty string")
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("unmarshal returned profile: %v", err)
	}
	if back["goalId"] != "g-harness-1" {
		t.Errorf("goalId mismatch: %v", back["goalId"])
	}
}

func TestGoalHarnessOverwrite(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	g := types.Goal{
		ID: "g-harness-ow", Title: "ow", Status: types.GoalPending,
		CompletionContract: types.CompletionContract{MaxIterations: 2},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = s.UpsertGoal(ctx, g)

	_ = s.SaveGoalHarness(ctx, "g-harness-ow", `{"v":1}`)
	_ = s.SaveGoalHarness(ctx, "g-harness-ow", `{"v":2}`)

	got, _ := s.LoadGoalHarness(ctx, "g-harness-ow")
	var m map[string]any
	_ = json.Unmarshal([]byte(got), &m)
	if int(m["v"].(float64)) != 2 {
		t.Errorf("expected v=2 after overwrite, got %v", m["v"])
	}
}

// ── Card harness persist round-trip ──────────────────────────────────────────

func TestCardHarnessRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	c := types.Card{
		ID: "c-harness-1", Title: "card", Column: types.ColBacklog,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.UpsertCard(ctx, c); err != nil {
		t.Fatalf("UpsertCard: %v", err)
	}

	profile := `{"cardId":"c-harness-1","current":{"mode":"manual","context":1}}`
	if err := s.SaveCardHarness(ctx, "c-harness-1", profile); err != nil {
		t.Fatalf("SaveCardHarness: %v", err)
	}

	got, err := s.LoadCardHarness(ctx, "c-harness-1")
	if err != nil {
		t.Fatalf("LoadCardHarness: %v", err)
	}
	if got != profile {
		t.Errorf("round-trip mismatch\nwant: %s\ngot:  %s", profile, got)
	}
}

// ── Harness mutations persist ─────────────────────────────────────────────────

func TestHarnessMutationsRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	g := types.Goal{
		ID: "g-mut-1", Title: "mut goal", Status: types.GoalRunning,
		CompletionContract: types.CompletionContract{MaxIterations: 5},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = s.UpsertGoal(ctx, g)

	now := time.Now().UTC()
	rows := []store.HarnessMutationRow{
		{
			ID: "m1", GoalID: "g-mut-1", CardID: "c1",
			Iteration: 2, Reason: "Direct→PlanExecute: repeated errors",
			OldProfile: `{"execution":1}`, NewProfile: `{"execution":3}`,
			Result: "pending", CreatedAt: now,
		},
		{
			ID: "m2", GoalID: "g-mut-1", CardID: "c1",
			Iteration: 4, Reason: "added IndependentReview",
			OldProfile: `{"verify":2}`, NewProfile: `{"verify":6}`,
			Result: "applied", CreatedAt: now.Add(time.Second),
		},
	}
	for _, r := range rows {
		if err := s.InsertHarnessMutation(ctx, r); err != nil {
			t.Fatalf("InsertHarnessMutation: %v", err)
		}
	}

	got, err := s.ListHarnessMutations(ctx, "g-mut-1")
	if err != nil {
		t.Fatalf("ListHarnessMutations: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 mutations, got %d", len(got))
	}
	if got[0].Reason != rows[0].Reason {
		t.Errorf("mutation[0] reason mismatch: %q", got[0].Reason)
	}
	if got[1].Iteration != 4 {
		t.Errorf("mutation[1] iteration mismatch: %d", got[1].Iteration)
	}
	if got[0].OldProfile != `{"execution":1}` {
		t.Errorf("mutation[0] OldProfile mismatch: %s", got[0].OldProfile)
	}
}

func TestHarnessMutationsEmptyForNewGoal(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	g := types.Goal{
		ID: "g-new", Title: "new", Status: types.GoalPending,
		CompletionContract: types.CompletionContract{MaxIterations: 2},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = s.UpsertGoal(ctx, g)

	rows, err := s.ListHarnessMutations(ctx, "g-new")
	if err != nil {
		t.Fatalf("ListHarnessMutations: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 mutations for new goal, got %d", len(rows))
	}
}

// ── Load harness not found ────────────────────────────────────────────────────

func TestLoadGoalHarnessNotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	g := types.Goal{
		ID: "g-nohp", Title: "nohp", Status: types.GoalPending,
		CompletionContract: types.CompletionContract{MaxIterations: 1},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = s.UpsertGoal(ctx, g)

	got, err := s.LoadGoalHarness(ctx, "g-nohp")
	if err != nil {
		t.Fatalf("LoadGoalHarness: %v", err)
	}
	// Empty string is expected when no harness has been saved yet.
	if got != "" {
		t.Errorf("expected empty string for unsaved harness, got: %s", got)
	}
}