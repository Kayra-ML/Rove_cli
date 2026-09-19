package store

import (
	"context"
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

	card := types.Card{ID: "c1", Title: "build", Column: types.ColReady, AcceptanceCriteria: []string{"tests pass"}, CreatedAt: now, UpdatedAt: now}
	if err := s.UpsertCard(ctx, card); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetCard(ctx, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.AcceptanceCriteria) != 1 {
		t.Fatalf("criteria %+v", c.AcceptanceCriteria)
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

func TestFileLease_ConflictAndExpiry(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "lease.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	until := time.Now().Add(time.Hour)
	if err := s.AcquireLease(ctx, "/src/main.go", "agent-a", "c1", until); err != nil {
		t.Fatal(err)
	}
	err = s.AcquireLease(ctx, "/src/main.go", "agent-b", "c2", until)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if err := s.ReleaseLease(ctx, "/src/main.go", "agent-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.AcquireLease(ctx, "/src/main.go", "agent-b", "c2", until); err != nil {
		t.Fatal(err)
	}

	past := time.Now().Add(-time.Minute)
	if err := s.AcquireLease(ctx, "/old.go", "agent-a", "c1", past); err != nil {
		t.Fatal(err)
	}
	if err := s.SweepExpiredLeases(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.AcquireLease(ctx, "/old.go", "agent-b", "c2", until); err != nil {
		t.Fatal(err)
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
