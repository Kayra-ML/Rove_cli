package staff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/gitwt"
	"github.com/Kayra-ML/rove/internal/types"
)

func repoFor(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"} {
		t.Setenv(k, v)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	g := gitwt.New()
	if err := g.Init(repo, "main"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(repo, "app.go"), []byte("package app\n"), 0o644)
	if err := g.CommitAll(repo, "init"); err != nil {
		t.Fatal(err)
	}
	return repo
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, _ := os.ReadFile(path)
	return string(b)
}

// A task works in a checkout of its own and leaves the user's folder alone;
// its changes wait to be applied or thrown away. A colleague's request
// lands at once, since the colleague needs it.
func TestTasksWorkApartUntilTheUserSays(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	repo := repoFor(t)
	e.Git, e.WorktreeRoot = gitwt.New(), filepath.Join(t.TempDir(), "wt")
	e.WorkspacePath = func(context.Context, types.ID) string { return repo }
	ali := hire(t, st, "Ali", "")
	// the user's own uncommitted work is in the copy the agent starts from
	_ = os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("mine\n"), 0o644)

	var seen []string
	e.Run = func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
		seen = append(seen, req.Workspace)
		if req.Workspace == repo {
			t.Errorf("the task ran in the user's folder")
		}
		if read(t, filepath.Join(req.Workspace, "notes.txt")) != "mine\n" {
			t.Errorf("the copy lacks the user's uncommitted work")
		}
		name := "feature.go"
		if strings.Contains(req.UserMessage, "ikinci") {
			name = "second.go"
		}
		if strings.Contains(req.UserMessage, "üçüncü") {
			name = "third.go"
		}
		if strings.Contains(req.UserMessage, "dördüncü") {
			name = "fourth.go"
		}
		_ = os.WriteFile(filepath.Join(req.Workspace, name), []byte("package app\n// done\n"), 0o644)
		return agent.RunResult{Assistant: "eklendi", Done: true}, nil
	}

	task, err := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "Özellik ekle", WorkspaceID: "w"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { x, _ := e.Get(ctx, task.ID); return x.Status == StatusDone })
	got, _ := e.Get(ctx, task.ID)
	if got.Isolation == nil || got.Isolation.State != IsoReview || strings.Join(got.Isolation.Files, ",") != "feature.go" || got.Isolation.Added != 2 {
		t.Fatalf("isolation = %+v", got.Isolation)
	}
	if _, err := os.Stat(filepath.Join(repo, "feature.go")); err == nil {
		t.Fatal("the work reached the user's folder before they said so")
	}
	if diff, err := e.Diff(ctx, task.ID); err != nil || !strings.Contains(diff, "+// done") {
		t.Fatalf("diff = %q, %v", diff, err)
	}
	if _, err := e.Apply(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(repo, "feature.go")) != "package app\n// done\n" {
		t.Fatal("applying did not land the work")
	}
	got, _ = e.Get(ctx, task.ID)
	if got.Isolation.State != IsoApplied || got.Isolation.Path != "" {
		t.Fatalf("after apply = %+v", got.Isolation)
	}

	// thrown away: nothing lands, the checkout goes
	second, _ := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "ikinci iş", WorkspaceID: "w"})
	waitFor(t, func() bool { x, _ := e.Get(ctx, second.ID); return x.Status == StatusDone })
	got, _ = e.Get(ctx, second.ID)
	path := got.Isolation.Path
	if _, err := e.Discard(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "second.go")); err == nil {
		t.Fatal("discarded work landed")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the discarded checkout is still there")
	}
	// no remote to push to: opening a PR fails, plainly, and nothing is lost
	third, _ := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "üçüncü iş", WorkspaceID: "w"})
	waitFor(t, func() bool { x, _ := e.Get(ctx, third.ID); return x.Status == StatusDone })
	if _, err := e.OpenPR(ctx, third.ID); err == nil || !strings.Contains(err.Error(), "push") {
		t.Fatalf("pr without a remote: %v", err)
	}
	got, _ = e.Get(ctx, third.ID)
	if got.Isolation.State != IsoReview || got.Isolation.Note == "" {
		t.Fatalf("after a failed pr = %+v", got.Isolation)
	}
	if out, _ := exec.Command("git", "-C", repo, "branch", "--list", "rove/*").Output(); strings.Contains(string(out), "rove/") && !strings.Contains(string(out), "rove/agent-") {
		t.Fatalf("a pr branch was left behind: %s", out)
	}

	// a colleague's request lands at once
	ayse := hire(t, st, "Ayşe", "")
	asked, err := e.AssignWait(ctx, AssignOpts{ProfileID: ayse.ID, Brief: "dördüncü iş", WorkspaceID: "w", From: ali.ID})
	if err != nil || asked.Isolation == nil || asked.Isolation.State != IsoApplied {
		t.Fatalf("colleague task = %+v, %v", asked.Isolation, err)
	}
	if read(t, filepath.Join(repo, "fourth.go")) == "" {
		t.Fatal("the colleague's work did not land")
	}
	if len(seen) != 4 {
		t.Fatalf("runs = %d", len(seen))
	}
}

// Without git the task works in the folder, as before.
func TestNoGitWorksInPlace(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	dir := t.TempDir()
	e.Git, e.WorktreeRoot = gitwt.New(), filepath.Join(t.TempDir(), "wt")
	e.WorkspacePath = func(context.Context, types.ID) string { return dir }
	ali := hire(t, st, "Ali", "")
	var ws string
	e.Run = func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
		ws = req.Workspace
		return agent.RunResult{Done: true}, nil
	}
	task, _ := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "iş", WorkspaceID: "w"})
	waitFor(t, func() bool { x, _ := e.Get(ctx, task.ID); return x.Status == StatusDone })
	got, _ := e.Get(ctx, task.ID)
	if ws != dir || got.Isolation != nil {
		t.Fatalf("workspace %q, isolation %+v", ws, got.Isolation)
	}
}
