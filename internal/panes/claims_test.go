package panes

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

func hub(t *testing.T) (*Hub, *session.Manager, types.Session) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sm := session.New(st, nil)
	s, err := sm.Create(context.Background(), "", "ag", "")
	if err != nil {
		t.Fatal(err)
	}
	h := New(st, sm)
	h.Running = func(types.ID) bool { return true }
	return h, sm, s
}

// Terminals have always guarded each other's files. A team's members write
// the same workspace as each other, and used to do it unguarded: the guard
// only looked at terminals, so two members told to edit one file both wrote
// it and the second quietly won.
func TestTeamMembersGuardEachOthersFiles(t *testing.T) {
	ctx := context.Background()
	h, sm, s := hub(t)
	a, err := sm.CreateChild(ctx, s, "Backend")
	if err != nil {
		t.Fatal(err)
	}
	b, err := sm.CreateChild(ctx, s, "Frontend")
	if err != nil {
		t.Fatal(err)
	}
	if held := h.Claim(ctx, a.ID, "/w", "src/app.go"); held != "" {
		t.Fatalf("the first writer was turned away by %q", held)
	}
	held := h.Claim(ctx, b.ID, "/w", "src/app.go")
	if held == "" {
		t.Fatal("the second member was let into a file the first is editing")
	}
	if held != "Backend" {
		t.Fatalf("it should be told who holds it, got %q", held)
	}
	// a different file is its own business
	if held := h.Claim(ctx, b.ID, "/w", "src/other.go"); held != "" {
		t.Fatalf("an untouched file was refused by %q", held)
	}
	// and the file frees up when its holder's run ends
	h.Release(a.ID)
	if held := h.Claim(ctx, b.ID, "/w", "src/app.go"); held != "" {
		t.Fatalf("the file stayed locked after its holder finished: %q", held)
	}
}

// Nothing to guard against when the session is working on its own.
func TestLoneSessionIsNotGuarded(t *testing.T) {
	ctx := context.Background()
	h, _, s := hub(t)
	if held := h.Claim(ctx, s.ID, "/w", "src/app.go"); held != "" {
		t.Fatalf("a session alone was refused by %q", held)
	}
}

// The terminal deck's own naming must survive the change: a terminal is
// still told which terminal holds the file, not the session's title.
func TestTerminalsStillNameEachOther(t *testing.T) {
	ctx := context.Background()
	h, sm, s := hub(t)
	t2, err := sm.CreateChildIn(ctx, s, types.SpaceTerminal, "Terminal 2")
	if err != nil {
		t.Fatal(err)
	}
	if held := h.Claim(ctx, t2.ID, "/w", "main.go"); held != "" {
		t.Fatalf("the first writer was turned away by %q", held)
	}
	held := h.Claim(ctx, s.ID, "/w", "main.go")
	if held != "T2" {
		t.Fatalf("holder label = %q, want T2", held)
	}
}
