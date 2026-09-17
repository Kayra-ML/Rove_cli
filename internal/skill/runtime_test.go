package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifestAndInstall(t *testing.T) {
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
	dest := filepath.Join(t.TempDir(), "skills")
	rt := New(nil, dest)
	sk, err := rt.InstallFromDir(src)
	if err != nil {
		t.Fatal(err)
	}
	if sk.Manifest.Name != "demo-skill" {
		t.Fatalf("%+v", sk)
	}
	if !sk.Manifest.Permissions.Filesystem {
		t.Fatal("expected filesystem permission declared")
	}
	caps := sk.Manifest.Permissions.DangerousCapabilities()
	if len(caps) != 1 || caps[0] != "filesystem" {
		t.Fatalf("caps %v", caps)
	}
	cmds := rt.SlashCommands()
	if len(cmds) != 1 || cmds[0].Name != "/demo" {
		t.Fatalf("%+v", cmds)
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
