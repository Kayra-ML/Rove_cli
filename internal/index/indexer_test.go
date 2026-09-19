package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func newTestIndexer(t *testing.T) *Indexer {
	t.Helper()
	dir := t.TempDir()
	idx, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

func TestNewAndClose(t *testing.T) {
	idx := newTestIndexer(t)
	if idx.IsIndexed() {
		t.Fatal("expected not indexed before first walk")
	}
}

func TestIndexWorkspaceAndSearch(t *testing.T) {
	idx := newTestIndexer(t)
	root := t.TempDir()

	// Write two source files
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc Frob() {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "util.go"), []byte("package main\n\nfunc Helper() {}"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := idx.IndexWorkspace(ctx, root); err != nil {
		t.Fatalf("IndexWorkspace: %v", err)
	}
	if !idx.IsIndexed() {
		t.Fatal("expected indexed after walk")
	}

	results, err := idx.Search(ctx, "Frob", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'Frob'")
	}
	if results[0].Path != "main.go" {
		t.Fatalf("path = %q want main.go", results[0].Path)
	}
}

func TestIndexSkipsBinaryExtensions(t *testing.T) {
	idx := newTestIndexer(t)
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "image.png"), []byte("\x89PNG\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "code.go"), []byte("package main // searchable"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := idx.IndexWorkspace(ctx, root); err != nil {
		t.Fatal(err)
	}

	all, err := idx.Search(ctx, "searchable", 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range all {
		if r.Path == "image.png" {
			t.Fatal("binary file was indexed")
		}
	}
	if len(all) == 0 {
		t.Fatal("expected code.go to be indexed")
	}
}

func TestIndexSkipsNodeModules(t *testing.T) {
	idx := newTestIndexer(t)
	root := t.TempDir()

	nm := filepath.Join(root, "node_modules", "pkg")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nm, "index.js"), []byte("// secret xyzzy"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := idx.IndexWorkspace(ctx, root); err != nil {
		t.Fatal(err)
	}

	results, err := idx.Search(ctx, "xyzzy", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("node_modules content should not be indexed, got %v", results)
	}
}

func TestSearchDefaultLimit(t *testing.T) {
	idx := newTestIndexer(t)
	root := t.TempDir()
	for i := 0; i < 15; i++ {
		name := filepath.Join(root, "f"+string(rune('a'+i))+".go")
		if err := os.WriteFile(name, []byte("package main // buzzword"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if err := idx.IndexWorkspace(ctx, root); err != nil {
		t.Fatal(err)
	}
	results, err := idx.Search(ctx, "buzzword", 0) // limit 0 → default 10
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 10 {
		t.Fatalf("default limit should cap at 10, got %d", len(results))
	}
}

func TestSkipFileFn(t *testing.T) {
	skipped := []string{"image.png", "image.jpg", "font.woff2", "go.sum", "package-lock.json", "binary.exe"}
	for _, name := range skipped {
		if !skipFile(name) {
			t.Errorf("skipFile(%q) = false want true", name)
		}
	}
	allowed := []string{"main.go", "util.ts", "style.css", "README.md"}
	for _, name := range allowed {
		if skipFile(name) {
			t.Errorf("skipFile(%q) = true want false", name)
		}
	}
}

func TestSkipDirFn(t *testing.T) {
	skipped := []string{".git", "node_modules", "dist", "build", ".next", "vendor"}
	for _, name := range skipped {
		if !skipDir(name) {
			t.Errorf("skipDir(%q) = false want true", name)
		}
	}
	if skipDir("src") || skipDir("internal") {
		t.Error("src/internal should not be skipped")
	}
}