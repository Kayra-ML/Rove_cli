package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

func TestOpenMemory_CRUD(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "aether.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	a := types.Agent{ID: "a1", Name: "lead", Profile: "lead", Model: "fake", Provider: "fake", Status: types.AgentIdle, CreatedAt: now, UpdatedAt: now}
	if err := s.UpsertAgent(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAgent(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "lead" {
		t.Fatalf("got %+v", got)
	}

	sess := types.Session{ID: "s1", Title: "chat", AgentID: "a1", CreatedAt: now, UpdatedAt: now}
	if err := s.UpsertSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertMessage(ctx, types.Message{ID: "m1", SessionID: "s1", Role: types.RoleUser, Content: "hi", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.ListMessages(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hi" {
		t.Fatalf("msgs %+v", msgs)
	}

	g := types.Goal{ID: "g1", Title: "ship", Status: types.GoalPending, CompletionContract: types.CompletionContract{Criteria: []string{"ok"}, MaxIterations: 3}, CreatedAt: now, UpdatedAt: now}
	if err := s.UpsertGoal(ctx, g); err != nil {
		t.Fatal(err)
	}
	gg, err := s.GetGoal(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if gg.CompletionContract.MaxIterations != 3 {
		t.Fatalf("goal %+v", gg)
	}

	if err := s.PutKV(ctx, "schema", "1"); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetKV(ctx, "schema")
	if err != nil || v != "1" {
		t.Fatalf("kv %s %v", v, err)
	}
	_, err = s.GetKV(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestMemoryUniqueKey(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "mem.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	e := types.MemoryEntry{ID: "m1", Scope: types.MemGlobal, Key: "pref", Content: "a", CreatedAt: time.Now()}
	if err := s.PutMemory(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.ID = "m2"
	e.Content = "b"
	if err := s.PutMemory(ctx, e); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMemory(ctx, types.MemGlobal, "", "pref")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "b" {
		t.Fatalf("got %s", got.Content)
	}
}

// A database made by an older version still has card_id columns on
// goals and harness mutations; they have defaults, so writing without them
// works and nothing has to be migrated.
func TestOldCardColumnsStillWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE goals (id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', contract TEXT NOT NULL,
			status TEXT NOT NULL, card_id TEXT NOT NULL DEFAULT '', workspace_id TEXT NOT NULL DEFAULT '', agent_id TEXT NOT NULL DEFAULT '',
			iteration INTEGER NOT NULL DEFAULT 0, last_verdict TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE harness_mutations (id TEXT PRIMARY KEY, goal_id TEXT NOT NULL, card_id TEXT NOT NULL DEFAULT '',
			iteration INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '', old_profile TEXT NOT NULL DEFAULT '{}',
			new_profile TEXT NOT NULL DEFAULT '{}', result TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.UpsertGoal(ctx, types.Goal{ID: "g1", Title: "ship", Status: types.GoalPending, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if g, err := s.GetGoal(ctx, "g1"); err != nil || g.Title != "ship" {
		t.Fatalf("goal = %+v, %v", g, err)
	}
	if err := s.InsertHarnessMutation(ctx, HarnessMutationRow{ID: "m1", GoalID: "g1", Reason: "r", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if ms, err := s.ListHarnessMutations(ctx, "g1"); err != nil || len(ms) != 1 {
		t.Fatalf("mutations = %+v, %v", ms, err)
	}
}
