package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AETHER_HOME", dir)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != dir && cfg.ListenHTTP == "" {
		t.Fatalf("%+v", cfg)
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
