package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadDefaultsAndOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ROVECODE_HOME", dir)
	t.Setenv("AETHER_HOME", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != dir {
		t.Fatalf("data dir = %s want %s", cfg.DataDir, dir)
	}
	p := filepath.Join(dir, "config.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"listenHttp":"127.0.0.1:9"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenHTTP != "127.0.0.1:9" {
		t.Fatalf("%s", cfg.ListenHTTP)
	}
}

func TestDefaultDataDirPrefersRoveThenLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ROVECODE_HOME", "")
	t.Setenv("AETHER_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	got := DefaultDataDir()
	preferred := filepath.Join(home, ".local", "share", "rovecode")
	if runtime.GOOS == "darwin" {
		preferred = filepath.Join(home, "Library", "Application Support", "Rove Code")
	}
	if runtime.GOOS == "windows" {
		preferred = filepath.Join(home, "AppData", "Roaming", "Rove Code")
	}
	if got != preferred {
		t.Fatalf("preferred = %s want %s", got, preferred)
	}
	legacy := filepath.Join(home, ".local", "share", "aether")
	if runtime.GOOS == "darwin" {
		legacy = filepath.Join(home, "Library", "Application Support", "Aether")
	}
	if runtime.GOOS == "windows" {
		legacy = filepath.Join(home, "AppData", "Roaming", "Aether")
	}
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	got = DefaultDataDir()
	if got != legacy {
		t.Fatalf("legacy fallback = %s want %s", got, legacy)
	}
}

func TestDBPathPrefersExistingLegacy(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "aether.db")
	if err := os.WriteFile(legacy, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DBPath(dir); got != legacy {
		t.Fatalf("%s", got)
	}
}
