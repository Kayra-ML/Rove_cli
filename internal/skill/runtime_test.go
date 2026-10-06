package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifest(t *testing.T) {
	src := t.TempDir()
	man := `
name: demo-skill
version: 1.0.0
author: aether
description: demo
requiredTools: [read_file]
permissions:
  filesystem: true
  shell: false
  network: false
  browser: false
  git: false
commands:
  - name: /demo
    description: run demo
    prompt: do the demo
`
	if err := os.WriteFile(filepath.Join(src, "skill.yaml"), []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(filepath.Join(src, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "demo-skill" {
		t.Fatalf("%+v", m)
	}
	if !m.Permissions.Filesystem {
		t.Fatal("expected filesystem permission declared")
	}
	caps := m.Permissions.DangerousCapabilities()
	if len(caps) != 1 || caps[0] != "filesystem" {
		t.Fatalf("caps %v", caps)
	}
	if len(m.Commands) != 1 || m.Commands[0].Name != "/demo" {
		t.Fatalf("%+v", m.Commands)
	}
}

func TestManifestRequiresNameAndVersion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "skill.yaml")
	if err := os.WriteFile(p, []byte("description: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(p); err == nil {
		t.Fatal("expected error")
	}
}
