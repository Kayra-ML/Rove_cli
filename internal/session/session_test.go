package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/types"
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
