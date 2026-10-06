package rpc

import (
	"os"
	"testing"
)

// A desktop app started from the Finder runs in "/": a chat without a folder
// must not get the whole disk as its workspace.
func TestFallbackDirSkipsDiskRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home")
	}
	t.Chdir("/")
	if got := fallbackDir(); got != home {
		t.Fatalf("fallbackDir in / = %q, want home %q", got, home)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if got := fallbackDir(); got == home || got == "/" {
		t.Fatalf("fallbackDir in %s = %q, want that folder", dir, got)
	}
}
