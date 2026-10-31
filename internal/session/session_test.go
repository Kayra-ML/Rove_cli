package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Burak-ML/rove/internal/store"
	"github.com/Burak-ML/rove/internal/types"
)

func TestCreateAppendHistory(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := New(s, nil)
	ctx := context.Background()
	sess, err := m.Create(ctx, "", "a1", "w1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Title != "New chat" {
		t.Fatalf("%s", sess.Title)
	}
	if _, err := m.Append(ctx, types.Message{SessionID: sess.ID, Role: types.RoleUser, Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Append(ctx, types.Message{SessionID: sess.ID, Role: types.RoleAssistant, Content: "yo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Append(ctx, types.Message{SessionID: sess.ID, Role: types.RoleUser, Content: "again"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Append(ctx, types.Message{SessionID: sess.ID, Role: types.RoleAssistant, Content: "ok"}); err != nil {
		t.Fatal(err)
	}
	hist, err := m.History(ctx, sess.ID)
	if err != nil || len(hist) != 4 {
		t.Fatalf("%v %v", hist, err)
	}
	if err := m.Truncate(ctx, sess.ID, 2); err != nil {
		t.Fatal(err)
	}
	hist, err = m.History(ctx, sess.ID)
	if err != nil || len(hist) != 2 {
		t.Fatalf("after undo %d %v", len(hist), err)
	}
	if hist[0].Content != "hi" || hist[1].Content != "yo" {
		t.Fatalf("%v", hist)
	}
	if err := m.Truncate(ctx, sess.ID, 0); err != nil {
		t.Fatal(err)
	}
	hist, err = m.History(ctx, sess.ID)
	if err != nil || len(hist) != 0 {
		t.Fatalf("clear %d %v", len(hist), err)
	}
}

func TestModeIsFixedOnceSet(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := New(s, nil)
	ctx := context.Background()
	sess, err := m.CreateMode(ctx, types.SpaceChat, types.ModeTeamwork, "", "a1", "w1")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := m.Get(ctx, sess.ID)
	if got.Mode != types.ModeTeamwork {
		t.Fatalf("mode %q", got.Mode)
	}
	// renaming (an upsert) keeps it
	if err := m.Rename(ctx, sess.ID, "x"); err != nil {
		t.Fatal(err)
	}
	if got, _ = m.Get(ctx, sess.ID); got.Mode != types.ModeTeamwork {
		t.Fatalf("rename changed mode to %q", got.Mode)
	}
	// it cannot be changed, only repeated
	if err := m.SetMode(ctx, sess.ID, types.ModeTerminal); !errors.Is(err, store.ErrModeFixed) {
		t.Fatalf("change allowed: %v", err)
	}
	if err := m.SetMode(ctx, sess.ID, types.ModeTeamwork); err != nil {
		t.Fatal(err)
	}
	// an old session without a mode gets one, once
	old, _ := m.Create(ctx, "", "a1", "w1")
	if err := m.SetMode(ctx, old.ID, types.ModeTerminal); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMode(ctx, old.ID, types.ModeOrchestra); !errors.Is(err, store.ErrModeFixed) {
		t.Fatalf("second set allowed: %v", err)
	}
	if got, _ = m.Get(ctx, old.ID); got.Mode != types.ModeTerminal {
		t.Fatalf("mode %q", got.Mode)
	}
}
