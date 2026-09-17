package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aether-dev/aether/internal/store"
)

func TestOpenIdempotentByPath(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := New(s, nil)
	ctx := context.Background()
	dir := t.TempDir()
	a, err := m.Open(ctx, dir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Open(ctx, dir, "other")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("%s vs %s", a.ID, b.ID)
	}
}
