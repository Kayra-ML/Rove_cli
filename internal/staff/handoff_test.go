package staff

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/gitwt"
	"github.com/Kayra-ML/rove/internal/types"
)

// Paired agents: when Ali finishes, Ayşe takes it on from his changes, and
// the user reviews both in one place. Chains stop at three; nobody gets
// work back from a chain it is in; failures are not handed on.
func TestHandoffCarriesTheWorkOn(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	repo := repoFor(t)
	e.Git, e.WorktreeRoot = gitwt.New(), filepath.Join(t.TempDir(), "wt")
	e.WorkspacePath = func(context.Context, types.ID) string { return repo }
	ali := hire(t, st, "Ali", "Backend")
	ayse := hire(t, st, "Ayşe", "Test")
	can := hire(t, st, "Can", "Docs")

	var mu sync.Mutex
	saw := map[string]bool{}
	var briefs []string
	e.Run = func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
		mu.Lock()
		briefs = append(briefs, req.UserMessage)
		mu.Unlock()
		switch {
		case strings.Contains(req.UserMessage, "colleague Ali just finished"):
			// Ayşe sees Ali's work in her copy
			if _, err := os.Stat(filepath.Join(req.Workspace, "feature.go")); err == nil {
				mu.Lock()
				saw["feature"] = true
				mu.Unlock()
			}
			_ = os.WriteFile(filepath.Join(req.Workspace, "feature_test.go"), []byte("package app\n"), 0o644)
		case strings.Contains(req.UserMessage, "colleague Ayşe just finished"):
			_ = os.WriteFile(filepath.Join(req.Workspace, "README.md"), []byte("docs\n"), 0o644)
		default:
			_ = os.WriteFile(filepath.Join(req.Workspace, "feature.go"), []byte("package app\n// f\n"), 0o644)
		}
		return agent.RunResult{Assistant: "bitti", Done: true}, nil
	}
	h1, err := e.SaveHandoff(ctx, Handoff{FromID: ali.ID, ToID: ayse.ID, Instruction: "Test yaz"})
	if err != nil || !h1.Enabled {
		t.Fatalf("handoff = %+v, %v", h1, err)
	}
	if again, _ := e.SaveHandoff(ctx, Handoff{FromID: ali.ID, ToID: ayse.ID}); again.ID != h1.ID {
		t.Fatal("the same pair made twice")
	}
	if _, err := e.SaveHandoff(ctx, Handoff{FromID: ali.ID, ToID: ali.ID}); err == nil {
		t.Fatal("an agent paired with itself")
	}
	_, _ = e.SaveHandoff(ctx, Handoff{FromID: ayse.ID, ToID: can.ID})
	_, _ = e.SaveHandoff(ctx, Handoff{FromID: can.ID, ToID: ali.ID}) // would close a loop

	first, _ := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "Özellik ekle", WorkspaceID: "w"})
	// Ali → Ayşe → Can, and there it stops
	waitFor(t, func() bool {
		x, _ := e.Tasks(ctx, can.ID, 0)
		return len(x) == 1 && x[0].Status == StatusDone
	})
	if x, _ := e.Tasks(ctx, ali.ID, 0); len(x) != 1 {
		t.Fatalf("the loop came back to Ali: %d tasks", len(x))
	}
	mu.Lock()
	if !saw["feature"] {
		t.Fatal("Ayşe did not start from Ali's changes")
	}
	mu.Unlock()
	ayseTask, _ := e.Tasks(ctx, ayse.ID, 0)
	if ayseTask[0].HandoffID != h1.ID || ayseTask[0].Origin != "Ali" || !strings.Contains(ayseTask[0].Brief, "Test yaz") || !strings.Contains(ayseTask[0].Brief, "feature.go") {
		t.Fatalf("Ayşe's task = %+v", ayseTask[0])
	}
	// the work moved along: Ali's and Ayşe's are carried; Can's holds all three
	got, _ := e.Get(ctx, first.ID)
	if got.Isolation.State != IsoCarried {
		t.Fatalf("Ali's work = %+v", got.Isolation)
	}
	if ayseTask[0].Isolation.State != IsoCarried {
		t.Fatalf("Ayşe's work = %+v", ayseTask[0].Isolation)
	}
	canTask, _ := e.Tasks(ctx, can.ID, 0)
	files := append([]string(nil), canTask[0].Isolation.Files...)
	sort.Strings(files)
	if canTask[0].Isolation.State != IsoReview || strings.Join(files, ",") != "README.md,feature.go,feature_test.go" {
		t.Fatalf("Can's review = %+v", canTask[0].Isolation)
	}
	// one apply brings all of it
	if _, err := e.Apply(ctx, canTask[0].ID); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"feature.go", "feature_test.go", "README.md"} {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Fatalf("%s did not land", f)
		}
	}
}

func TestFailuresAreNotHandedOn(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	ali := hire(t, st, "Ali", "")
	ayse := hire(t, st, "Ayşe", "")
	_, _ = e.SaveHandoff(ctx, Handoff{FromID: ali.ID, ToID: ayse.ID})
	e.Run = func(context.Context, agent.RunRequest) (agent.RunResult, error) {
		return agent.RunResult{}, context.DeadlineExceeded
	}
	task, _ := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "iş"})
	waitFor(t, func() bool { x, _ := e.Get(ctx, task.ID); return x.Status == StatusFailed })
	if x, _ := e.Tasks(ctx, ayse.ID, 0); len(x) != 0 {
		t.Fatal("a failed task was handed on")
	}
}
