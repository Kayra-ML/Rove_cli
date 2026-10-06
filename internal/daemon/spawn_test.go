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

// A GUI app launched from Finder has a bare PATH; the daemon installed in
// /usr/local/bin must still be found.
func TestCommandFindsWellKnownInstallWithBarePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix install paths")
	}
	bin := t.TempDir()
	want := writeStub(t, bin, "rovecode")
	t.Setenv("PATH", t.TempDir())
	prev := wellKnownDirs
	wellKnownDirs = func() []string { return []string{filepath.Join(t.TempDir(), "missing"), bin} }
	defer func() { wellKnownDirs = prev }()
	cmd := Command()
	if cmd == nil || cmd.Path != want || len(cmd.Args) != 2 || cmd.Args[1] != "daemon" {
		t.Fatalf("got %+v", cmd)
	}
}
