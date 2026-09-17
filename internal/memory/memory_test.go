package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/types"
)

func TestPromptBlockScopes(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sys := New(s)
	ctx := context.Background()
	if _, err := sys.Remember(ctx, types.MemGlobal, "", "tone", "direct"); err != nil {
		t.Fatal(err)
	}
	if _, err := sys.Remember(ctx, types.MemWorkspace, "w1", "stack", "go"); err != nil {
		t.Fatal(err)
	}
	block := sys.PromptBlock(ctx, "", "w1", "")
	if !strings.Contains(block, "direct") || !strings.Contains(block, "go") {
		t.Fatalf("%s", block)
	}
}
