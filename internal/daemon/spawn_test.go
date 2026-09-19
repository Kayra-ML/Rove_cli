package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeStub(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	body := "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		p += ".bat"
		body = "@echo off\r\n"
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCommandPrefersRoveCodeDaemonArg(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "rovecode")
	t.Setenv("PATH", dir)
	cmd := Command()
	if cmd == nil {
		t.Fatal("expected command")
	}
	found := false
	for _, a := range cmd.Args {
		if a == "daemon" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want daemon arg, got %v", cmd.Args)
	}
}

func TestCommandFallsBackToAetherd(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "aetherd")
	t.Setenv("PATH", dir)
	cmd := Command()
	if cmd == nil {
		t.Fatal("expected command")
	}
	for _, a := range cmd.Args {
		if a == "daemon" {
			t.Fatalf("legacy aetherd should not get daemon arg: %v", cmd.Args)
		}
	}
}
