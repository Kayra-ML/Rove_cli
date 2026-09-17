package permission

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/types"
)

func TestEvaluate_DefaultAsk(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := New(s)
	d, err := e.Evaluate(context.Background(), Check{Action: types.PermShell, Target: "rm -rf /"})
	if err != nil {
		t.Fatal(err)
	}
	if d != types.PermAsk {
		t.Fatalf("got %s", d)
	}
}

func TestEvaluate_SpecificBeatsWildcard(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := New(s)
	ctx := context.Background()
	if err := e.Put(ctx, types.PermissionRule{ID: "1", Action: types.PermFilesystem, Pattern: "*", Decision: types.PermDeny}); err != nil {
		t.Fatal(err)
	}
	if err := e.Put(ctx, types.PermissionRule{ID: "2", Action: types.PermFilesystem, Pattern: "/ws/**", Decision: types.PermAllow}); err != nil {
		t.Fatal(err)
	}
	d, err := e.Evaluate(ctx, Check{Action: types.PermFilesystem, Target: "/ws/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if d != types.PermAllow {
		t.Fatalf("got %s", d)
	}
	d, err = e.Evaluate(ctx, Check{Action: types.PermFilesystem, Target: "/etc/passwd"})
	if err != nil {
		t.Fatal(err)
	}
	if d != types.PermDeny {
		t.Fatalf("got %s", d)
	}
}
