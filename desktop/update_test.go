package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.2.10", "0.2.9", true},
		{"0.2.9", "0.2.10", false},
		{"v0.3.0", "0.2.99", true},
		{"0.2.10", "0.2.10", false},
		{"v1.0.0-rc1", "0.9", true},
		{"0.2", "0.2.0", false},
	} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestCheckSum(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.zip")
	_ = os.WriteFile(p, []byte("hello"), 0o600)
	sums := filepath.Join(dir, "SHA256SUMS")
	_ = os.WriteFile(sums, []byte("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824  a.zip\n"), 0o600)
	if err := checkSum(p, sums, "a.zip"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(p, []byte("tampered"), 0o600)
	if checkSum(p, sums, "a.zip") == nil {
		t.Fatal("tampered file passed")
	}
	if checkSum(p, sums, "b.zip") == nil {
		t.Fatal("missing entry passed")
	}
}

func TestSwapBundle(t *testing.T) {
	dir := t.TempDir()
	cur := filepath.Join(dir, "Rove Code.app")
	next := filepath.Join(t.TempDir(), "Rove Code.app")
	for p, v := range map[string]string{cur: "old", next: "new"} {
		_ = os.MkdirAll(filepath.Join(p, "Contents"), 0o755)
		_ = os.WriteFile(filepath.Join(p, "Contents", "v"), []byte(v), 0o644)
	}
	if err := swapBundle(cur, next); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(cur, "Contents", "v")); string(b) != "new" {
		t.Fatalf("after swap: %q", b)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("left behind: %v", ents)
	}
}
