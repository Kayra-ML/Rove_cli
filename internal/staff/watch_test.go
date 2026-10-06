package staff

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/types"
)

// An agent watching a chat gets a task when a turn there changes files it
// cares about — once at a time, up to its daily cap, never from its own work.
func TestWatchSetsOffATaskOnChanges(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	qa := hire(t, st, "Ayşe", "Test uzmanı")
	sm := session.New(st, nil)
	chat, _ := sm.CreateIn(ctx, types.SpaceChat, "Web", "ag", "")

	w, err := e.SaveWatch(ctx, Watch{SessionID: chat.ID, ProfileID: qa.ID, Instruction: "Testleri gözden geçir", Filter: "*.go"})
	if err != nil || !w.Enabled || w.ID == "" {
		t.Fatalf("watch = %+v, %v", w, err)
	}
	// drawing the cable again finds the same watch
	if again, _ := e.SaveWatch(ctx, Watch{SessionID: chat.ID, ProfileID: qa.ID}); again.ID != w.ID {
		t.Fatalf("a second watch for the same pair: %+v", again)
	}

	var mu sync.Mutex
	var briefs []string
	release := make(chan struct{})
	e.Run = func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
		mu.Lock()
		briefs = append(briefs, req.UserMessage)
		mu.Unlock()
		<-release
		return agent.RunResult{Assistant: "Testler geçiyor.", Done: true}, nil
	}
	run := func(sid types.ID, files ...string) {
		e.OnRunDone(agent.RunRequest{SessionID: sid, Workspace: "/p"}, agent.RunResult{FilesEdited: files, Assistant: "login eklendi"})
	}
	tasks := func() []Task { x, _ := e.Tasks(ctx, qa.ID, 0); return x }

	run(chat.ID, "/p/README.md") // the filter lets nothing through
	if len(tasks()) != 0 {
		t.Fatal("a change outside the filter set the watch off")
	}
	run(chat.ID, "/p/internal/auth/login.go", "/p/README.md")
	waitFor(t, func() bool { return len(tasks()) == 1 })
	first := tasks()[0]
	if first.WatchID != w.ID || first.Origin != "Web" || first.Seen {
		t.Fatalf("task = %+v", first)
	}
	// the task is recorded before its run starts: wait for the run
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(briefs) > 0 })
	mu.Lock()
	b := briefs[0]
	mu.Unlock()
	for _, want := range []string{"Testleri gözden geçir", `"Web"`, "- internal/auth/login.go", "login eklendi"} {
		if !strings.Contains(b, want) {
			t.Fatalf("brief lacks %q:\n%s", want, b)
		}
	}
	if strings.Contains(b, "README") {
		t.Fatalf("a filtered-out file is in the brief:\n%s", b)
	}
	// while that task runs, more changes do not pile up tasks
	run(chat.ID, "/p/a.go")
	if len(tasks()) != 1 {
		t.Fatalf("a second task while one runs: %d", len(tasks()))
	}
	close(release)
	waitFor(t, func() bool { return tasks()[0].Status == StatusDone })

	// the task's own chat changing files sets nothing off
	run(first.SessionID, "/p/b.go")
	if len(tasks()) != 1 {
		t.Fatal("an agent's own task set a watch off")
	}

	// switched off: nothing
	w.Enabled = false
	_, _ = e.SaveWatch(ctx, w)
	run(chat.ID, "/p/c.go")
	if len(tasks()) != 1 {
		t.Fatal("a switched-off watch set a task off")
	}

	// the daily cap
	w.Enabled, w.DailyLimit = true, 2
	_, _ = e.SaveWatch(ctx, w)
	run(chat.ID, "/p/d.go")
	waitFor(t, func() bool { x := tasks(); return len(x) == 2 && x[0].Status == StatusDone })
	run(chat.ID, "/p/e.go")
	if len(tasks()) != 2 {
		t.Fatalf("past the daily cap: %d tasks", len(tasks()))
	}
}

func TestMatching(t *testing.T) {
	files := []string{"/p/src/app.tsx", "/p/internal/x.go", "/p/docs/a.md"}
	for _, c := range []struct {
		filter string
		want   []string
	}{
		{"", []string{"src/app.tsx", "internal/x.go", "docs/a.md"}},
		{"*.go", []string{"internal/x.go"}},
		{"src/*, *.md", []string{"src/app.tsx", "docs/a.md"}},
	} {
		got := matching(c.filter, files, "/p")
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("matching(%q) = %v, want %v", c.filter, got, c.want)
		}
	}
}
