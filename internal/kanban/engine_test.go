package kanban

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/types"
)

func TestMoveBlockedByDependency(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := New(s, nil)
	ctx := context.Background()
	dep, err := e.Create(ctx, types.Card{Title: "dep", Column: types.ColBacklog})
	if err != nil {
		t.Fatal(err)
	}
	card, err := e.Create(ctx, types.Card{Title: "child", Column: types.ColBacklog, Dependencies: []types.ID{dep.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Move(ctx, card.ID, types.ColReady); err == nil {
		t.Fatal("expected dependency block")
	}
	if _, err := e.Move(ctx, dep.ID, types.ColDone); err != nil {
		t.Fatal(err)
	}
	moved, err := e.Move(ctx, card.ID, types.ColReady)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Column != types.ColReady {
		t.Fatalf("got %s", moved.Column)
	}
}

func TestReviewApproveGoesDone(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := New(s, nil)
	ctx := context.Background()
	c, err := e.Create(ctx, types.Card{Title: "r", Column: types.ColReview})
	if err != nil {
		t.Fatal(err)
	}
	c, err = e.SetReview(ctx, c.ID, types.ReviewApproved)
	if err != nil {
		t.Fatal(err)
	}
	if c.Column != types.ColDone {
		t.Fatalf("got %s", c.Column)
	}
}

func TestAssignAndSessionID(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := New(s, nil)
	ctx := context.Background()
	c, err := e.Create(ctx, types.Card{Title: "a", Column: types.ColBacklog, SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	if c.SessionID != "sess-1" {
		t.Fatalf("session %s", c.SessionID)
	}
	got, err := e.Assign(ctx, c.ID, "agent-9")
	if err != nil {
		t.Fatal(err)
	}
	if got.AssigneeAgentID != "agent-9" {
		t.Fatalf("assignee %s", got.AssigneeAgentID)
	}
	again, err := e.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.SessionID != "sess-1" || again.AssigneeAgentID != "agent-9" {
		t.Fatalf("%+v", again)
	}
}
