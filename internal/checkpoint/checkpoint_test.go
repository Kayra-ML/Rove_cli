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

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

// A snapshot is a copy: the tree, the index and untracked files stay put.
func TestTakeLeavesTheTreeAlone(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "two\n")
	write(t, dir, "staged.txt", "s\n")
	git(t, dir, "add", "staged.txt")
	write(t, dir, "new.txt", "fresh\n")
	if _, err := New().Take(dir, "edit"); err != nil {
		t.Fatal(err)
	}
	if read(t, dir, "a.txt") != "two\n" || read(t, dir, "new.txt") != "fresh\n" || read(t, dir, "staged.txt") != "s\n" {
		t.Fatalf("tree changed: a=%q new=%q", read(t, dir, "a.txt"), read(t, dir, "new.txt"))
	}
	if cached := git(t, dir, "diff", "--cached", "--name-only"); cached != "staged.txt\n" {
		t.Fatalf("index changed: %q", cached)
	}
}

// What the agent does: snapshot, edit, snapshot, edit. No edit may vanish.
func TestSnapshotsBeforeEachEditKeepEarlierEdits(t *testing.T) {
	dir := initRepo(t)
	m := New()
	_, _ = m.Take(dir, "auto:write_file") // clean tree: nothing to save
	write(t, dir, "a.txt", "two\n")
	if _, err := m.Take(dir, "auto:write_file"); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "b.txt", "bee\n")
	if _, err := m.Take(dir, "auto:shell"); err != nil {
		t.Fatal(err)
	}
	if read(t, dir, "a.txt") != "two\n" || read(t, dir, "b.txt") != "bee\n" {
		t.Fatalf("an earlier edit vanished: a=%q b=%q", read(t, dir, "a.txt"), read(t, dir, "b.txt"))
	}
}

// Deleted files are part of a snapshot too, both ways.
func TestSnapshotsCoverDeletions(t *testing.T) {
	dir := initRepo(t)
	m := New()
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Take(dir, "gone"); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "back\n")
	snaps, _ := m.List(dir)
	if err := m.Restore(dir, snaps[0].Ref); err != nil {
		t.Fatal(err)
	}
	if read(t, dir, "a.txt") != "<missing>" {
		t.Fatalf("a.txt should stay deleted, got %q", read(t, dir, "a.txt"))
	}
	snaps, _ = m.List(dir)
	if err := m.Restore(dir, snaps[0].Ref); err != nil { // undo: before-restore
		t.Fatal(err)
	}
	if read(t, dir, "a.txt") != "back\n" {
		t.Fatalf("undo restore: %q", read(t, dir, "a.txt"))
	}
}

// Snapshot commits carry their own identity: a repo without user.name /
// user.email configured (like a fresh machine) must still snapshot.
func TestTakeWithoutGitIdentity(t *testing.T) {
	dir := initRepo(t)
	git(t, dir, "config", "--unset", "user.email")
	git(t, dir, "config", "--unset", "user.name")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "none"))
	write(t, dir, "a.txt", "two\n")
	if _, err := New().Take(dir, "noid"); err != nil {
		t.Fatal(err)
	}
}

// Restore goes back to the snapshot — dropping later edits and new files —
// and saves what was there, so the restore itself can be undone.
func TestRestoreGoesBackAndCanBeUndone(t *testing.T) {
	dir := initRepo(t)
	m := New()
	write(t, dir, "a.txt", "two\n")
	if _, err := m.Take(dir, "s1"); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "three\n")
	write(t, dir, "late.txt", "late\n")

	snaps, _ := m.List(dir)
	if len(snaps) != 1 || snaps[0].Label != "s1" {
		t.Fatalf("list = %+v", snaps)
	}
	if err := m.Restore(dir, snaps[0].Ref); err != nil {
		t.Fatal(err)
	}
	if read(t, dir, "a.txt") != "two\n" || read(t, dir, "late.txt") != "<missing>" {
		t.Fatalf("after restore: a=%q late=%q", read(t, dir, "a.txt"), read(t, dir, "late.txt"))
	}
	snaps, _ = m.List(dir)
	if len(snaps) != 2 || snaps[0].Label != "before-restore" || snaps[1].Label != "s1" {
		t.Fatalf("list after restore = %+v", snaps)
	}
	// undo the restore
	if err := m.Restore(dir, snaps[0].Ref); err != nil {
		t.Fatal(err)
	}
	if read(t, dir, "a.txt") != "three\n" || read(t, dir, "late.txt") != "late\n" {
		t.Fatalf("after undoing restore: a=%q late=%q", read(t, dir, "a.txt"), read(t, dir, "late.txt"))
	}

	if err := m.Drop(dir, "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(dir, "stash@{9}"); err == nil {
		t.Fatal("restoring a missing snapshot should fail")
	}
}

// Snapshots made by the old `git stash push -u` still restore, untracked
// files included.
func TestRestoreLegacyStash(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "two\n")
	write(t, dir, "new.txt", "fresh\n")
	git(t, dir, "stash", "push", "--include-untracked", "-m", "legacy")
	if read(t, dir, "new.txt") != "<missing>" {
		t.Fatal("setup: stash push should have cleared the tree")
	}
	if err := New().Restore(dir, "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if read(t, dir, "a.txt") != "two\n" || read(t, dir, "new.txt") != "fresh\n" {
		t.Fatalf("legacy restore: a=%q new=%q", read(t, dir, "a.txt"), read(t, dir, "new.txt"))
	}
}

// Back-to-back snapshots of the same tree (one per agent tool call) do not
// pile up.
func TestIdenticalSnapshotsAreNotRepeated(t *testing.T) {
	dir := initRepo(t)
	m := New()
	write(t, dir, "a.txt", "two\n")
	for i := 0; i < 3; i++ {
		if _, err := m.Take(dir, "auto:shell"); err != nil {
			t.Fatal(err)
		}
	}
	write(t, dir, "a.txt", "three\n")
	if _, err := m.Take(dir, "auto:write_file"); err != nil {
		t.Fatal(err)
	}
	if snaps, _ := m.List(dir); len(snaps) != 2 {
		t.Fatalf("snapshots = %d, want 2", len(snaps))
	}
}
