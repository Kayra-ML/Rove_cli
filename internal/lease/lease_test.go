package lease

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/store"
)

func TestAcquireManyRollback(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := New(s, time.Minute)
	ctx := context.Background()
	if err := c.Acquire(ctx, "/a.go", "one", "c1"); err != nil {
		t.Fatal(err)
	}
	err = c.AcquireMany(ctx, []string{"/b.go", "/a.go"}, "two", "c2")
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("got %v", err)
	}
	if err := c.Acquire(ctx, "/b.go", "three", "c3"); err != nil {
		t.Fatal(err)
	}
}
