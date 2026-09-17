package gitwt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeIsolation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	m := New()
	if err := m.Init(repo, "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.AddAll(repo); err != nil {
		t.Fatal(err)
	}
	os.Setenv("GIT_AUTHOR_NAME", "aether")
	os.Setenv("GIT_AUTHOR_EMAIL", "aether@local")
	os.Setenv("GIT_COMMITTER_NAME", "aether")
	os.Setenv("GIT_COMMITTER_EMAIL", "aether@local")
	if err := m.Commit(repo, "init"); err != nil {
		t.Fatal(err)
	}
	wt, err := m.CreateWorktree(repo, m.WorktreePath(repo, "c1"), m.BranchForCard("c1"))
	if err != nil {
		t.Fatal(err)
	}
	if wt.Branch != "aether/c1" {
		t.Fatalf("branch %s", wt.Branch)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "agent.txt"), []byte("from agent"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := m.Status(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range st.Untracked {
		if c == "agent.txt" {
			t.Fatal("worktree file leaked into main")
		}
	}
	list, err := m.ListWorktrees(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 {
		t.Fatalf("want main+worktree, got %d", len(list))
	}
	if err := m.RemoveWorktree(repo, wt.Path); err != nil {
		t.Fatal(err)
	}
}

func TestDiffShowsUnstaged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	m := New()
	if err := m.Init(repo, "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.AddAll(repo); err != nil {
		t.Fatal(err)
	}
	os.Setenv("GIT_AUTHOR_NAME", "aether")
	os.Setenv("GIT_AUTHOR_EMAIL", "aether@local")
	os.Setenv("GIT_COMMITTER_NAME", "aether")
	os.Setenv("GIT_COMMITTER_EMAIL", "aether@local")
	if err := m.Commit(repo, "init"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := m.Diff(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "two") {
		t.Fatalf("diff missing change: %q", diff)
	}
}
