package checkpoint

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
	return string(out)
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "test@test.com")
	git(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "init")
	return dir
}

func TestTakeOnCleanTreeErrors(t *testing.T) {
	dir := initRepo(t)
	if _, err := New().Take(dir, "noop"); err == nil {
		t.Fatal("clean tree should not snapshot")
	}
}

func TestListEmpty(t *testing.T) {
	dir := initRepo(t)
	snaps, err := New().List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 0 {
		t.Fatalf("want 0 got %d", len(snaps))
	}
}

func TestTakeListRestoreDrop(t *testing.T) {
	dir := initRepo(t)
	m := New()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0644); err != nil {
		t.Fatal(err)
	}
	snap, err := m.Take(dir, "edit-a")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Ref == "" {
		t.Fatal("empty ref")
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "one\n" {
		t.Fatalf("working tree after take: %q", got)
	}

	snaps, err := m.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 {
		t.Fatalf("list %d", len(snaps))
	}
	if snaps[0].Label != "edit-a" {
		t.Fatalf("label %q", snaps[0].Label)
	}

	if err := m.Restore(dir, snaps[0].Ref); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "two\n" {
		t.Fatalf("restore: %q", got)
	}

	if err := m.Drop(dir, snaps[0].Ref); err != nil {
		t.Fatal(err)
	}
	snaps, err = m.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 0 {
		t.Fatalf("after drop %d %+v", len(snaps), snaps)
	}
}

func TestTakeIncludesUntracked(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("fresh\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if _, err := m.Take(dir, "untracked"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("untracked should be stashed away, stat=%v", err)
	}
	snaps, err := m.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 {
		t.Fatalf("list %d", len(snaps))
	}
	if err := m.Restore(dir, snaps[0].Ref); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fresh\n" {
		t.Fatalf("restore untracked: %q", got)
	}
}
