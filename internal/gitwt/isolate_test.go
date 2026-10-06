package gitwt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoWithCommit(t *testing.T) (*Manager, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
	} {
		t.Setenv(k, v)
	}
	m := New()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := m.Init(repo, "main"); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "a.txt", "one\ntwo\nthree\n")
	write(t, repo, ".gitignore", "secret.env\n")
	if err := m.CommitAll(repo, "init"); err != nil {
		t.Fatal(err)
	}
	return m, repo
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
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

// A subagent must start from what the user sees, not from the last commit:
// their uncommitted edit and their new file have to be in its checkout —
// and taking the snapshot must leave the user's index exactly as it was.
func TestSnapshotCarriesUncommittedWorkAndLeavesTheIndexAlone(t *testing.T) {
	m, repo := repoWithCommit(t)
	write(t, repo, "a.txt", "one\nTWO\nthree\n")
	write(t, repo, "new.txt", "fresh\n")
	write(t, repo, "secret.env", "TOKEN=1\n")
	staged, _ := m.run(repo, "diff", "--cached", "--name-only")

	snap, err := m.Snapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if after, _ := m.run(repo, "diff", "--cached", "--name-only"); after != staged {
		t.Fatalf("the user's index changed: %q -> %q", staged, after)
	}
	dest := filepath.Join(t.TempDir(), "wt")
	if err := m.AddWorktree(repo, dest, "rove/sub-1", snap); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dest, "a.txt"); got != "one\nTWO\nthree\n" {
		t.Fatalf("uncommitted edit missing in the checkout: %q", got)
	}
	if got := read(t, dest, "new.txt"); got != "fresh\n" {
		t.Fatalf("new file missing in the checkout: %q", got)
	}
	if got := read(t, dest, "secret.env"); got != "<missing>" {
		t.Fatal("an ignored file was copied into the checkout")
	}
}

func TestSnapshotOfACleanTreeIsHead(t *testing.T) {
	m, repo := repoWithCommit(t)
	head, _ := m.run(repo, "rev-parse", "HEAD")
	snap, err := m.Snapshot(repo)
	if err != nil || snap != head {
		t.Fatalf("snapshot = %q (%v), head = %q", snap, err, head)
	}
}

// The round trip: work in the checkout comes back to the user's tree as
// uncommitted changes, new files and deletions included, with its counts.
func TestWorkComesBackAsUncommittedChanges(t *testing.T) {
	m, repo := repoWithCommit(t)
	head, _ := m.run(repo, "rev-parse", "HEAD")
	snap, err := m.Snapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "wt")
	if err := m.AddWorktree(repo, dest, "rove/sub-2", snap); err != nil {
		t.Fatal(err)
	}
	write(t, dest, "a.txt", "one\ntwo\nthree\nfour\n")
	write(t, dest, "pkg/b.go", "package pkg\n")
	patch, added, removed, files, err := m.WorkPatch(dest, snap)
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 || removed != 0 || len(files) != 2 {
		t.Fatalf("counts +%d -%d files %v", added, removed, files)
	}
	if err := m.ApplyPatch(repo, patch); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("edit did not land: %q", got)
	}
	if got := read(t, repo, "pkg/b.go"); got != "package pkg\n" {
		t.Fatalf("new file did not land: %q", got)
	}
	if now, _ := m.run(repo, "rev-parse", "HEAD"); now != head {
		t.Fatal("the user's branch was committed to")
	}
	if err := m.DropWorktree(repo, dest, "rove/sub-2"); err != nil {
		t.Fatal(err)
	}
	if b, _ := m.run(repo, "branch", "--list", "rove/sub-2"); b != "" {
		t.Fatal("the subagent's branch was left behind")
	}
}

// Two subagents changed the same lines: the second patch must not land half
// way. Either all of it fits or nothing of it touches the user's tree.
func TestAPatchThatDoesNotFitChangesNothing(t *testing.T) {
	m, repo := repoWithCommit(t)
	snap, _ := m.Snapshot(repo)
	dest := filepath.Join(t.TempDir(), "wt")
	if err := m.AddWorktree(repo, dest, "rove/sub-3", snap); err != nil {
		t.Fatal(err)
	}
	write(t, dest, "a.txt", "one\nsubagent\nthree\n")
	write(t, dest, "other.txt", "also new\n")
	patch, _, _, _, err := m.WorkPatch(dest, snap)
	if err != nil {
		t.Fatal(err)
	}
	// meanwhile the same line changed in the user's tree
	write(t, repo, "a.txt", "one\nuser\nthree\n")
	err = m.ApplyPatch(repo, patch)
	if err == nil {
		t.Fatal("a conflicting patch was applied")
	}
	if got := read(t, repo, "a.txt"); !strings.Contains(got, "user") {
		t.Fatalf("the user's line was overwritten: %q", got)
	}
	if got := read(t, repo, "other.txt"); got != "<missing>" {
		t.Fatal("half of the patch landed")
	}
}
