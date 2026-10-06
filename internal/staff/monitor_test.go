package staff

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
)

// A monitor learns what is there on its first check, gives the agent a task
// only for lines it has not seen, costs nothing while nothing is new, waits
// while its last task runs, and says when its command fails.
func TestMonitorHandsOnlyNewItems(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	ali := hire(t, st, "Ali", "DevOps")
	var mu sync.Mutex
	output := "101 build failed [main]\n102 lint failed [dev]\n"
	var cmdErr error
	e.RunCommand = func(_ context.Context, command, dir string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return output, cmdErr
	}
	runs := 0
	release := make(chan struct{})
	var briefs []string
	e.Run = func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
		mu.Lock()
		runs++
		briefs = append(briefs, req.UserMessage)
		mu.Unlock()
		<-release
		return agent.RunResult{Done: true}, nil
	}
	m, err := e.SaveMonitor(ctx, Monitor{ProfileID: ali.ID, Name: "CI", Command: "gh run list", Instruction: "Düzelt", EveryMinutes: 1})
	if err != nil || m.EveryMinutes != minMonitorMinutes || !m.Enabled {
		t.Fatalf("monitor = %+v, %v", m, err)
	}
	t0 := time.Now()
	// the first check only learns
	if n := e.Tick(ctx, t0); n != 0 {
		t.Fatal("the first check gave a task for what was already there")
	}
	// too soon: no check at all
	mu.Lock()
	output += "103 test failed [main]\n"
	mu.Unlock()
	if n := e.Tick(ctx, t0.Add(time.Minute)); n != 0 {
		t.Fatal("checked before its interval")
	}
	// a new line: a task with that line only
	if n := e.Tick(ctx, t0.Add(6*time.Minute)); n != 1 {
		t.Fatal("a new line gave no task")
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(briefs) == 1 })
	mu.Lock()
	b := briefs[0]
	mu.Unlock()
	if !strings.Contains(b, "- 103 test failed [main]") || strings.Contains(b, "101") || !strings.Contains(b, "Düzelt") {
		t.Fatalf("brief = %s", b)
	}
	tasks, _ := e.Tasks(ctx, ali.ID, 0)
	if len(tasks) != 1 || tasks[0].MonitorID != m.ID || tasks[0].Origin != "CI" {
		t.Fatalf("task = %+v", tasks)
	}
	// while it runs, a further new line waits
	mu.Lock()
	output += "104 deploy failed\n"
	mu.Unlock()
	if n := e.Tick(ctx, t0.Add(12*time.Minute)); n != 0 {
		t.Fatal("a second task while the first runs")
	}
	close(release)
	waitFor(t, func() bool { x, _ := e.Tasks(ctx, ali.ID, 0); return x[0].Status == StatusDone })
	if n := e.Tick(ctx, t0.Add(18*time.Minute)); n != 1 {
		t.Fatal("the waiting line gave no task")
	}
	// nothing new: no task, no tokens
	if n := e.Tick(ctx, t0.Add(24*time.Minute)); n != 0 {
		t.Fatal("old lines gave a task")
	}
	// a failing command is said, not acted on
	mu.Lock()
	cmdErr = errors.New("gh: not logged in")
	mu.Unlock()
	e.Tick(ctx, t0.Add(30*time.Minute))
	ms, _ := e.Monitors(ctx)
	if ms[0].LastError != "gh: not logged in" {
		t.Fatalf("error = %q", ms[0].LastError)
	}
	// changing the command learns afresh
	mu.Lock()
	cmdErr = nil
	mu.Unlock()
	m.Command = "gh run list --limit 5"
	m, _ = e.SaveMonitor(ctx, m)
	if len(m.Seen) != 0 || !m.LastRun.IsZero() {
		t.Fatalf("after a new command = %+v", m)
	}
	if n := e.Tick(ctx, t0.Add(36*time.Minute)); n != 0 {
		t.Fatal("a new command's first check gave a task")
	}
}

func TestMonitorRunsTheRealShell(t *testing.T) {
	e, _ := setup(t)
	dir := t.TempDir()
	out, err := e.runCommand(context.Background(), "pwd; echo hello", dir)
	if err != nil || !strings.Contains(out, "hello") {
		t.Fatalf("out = %q, %v", out, err)
	}
	if _, err := e.runCommand(context.Background(), "echo nope >&2; exit 3", dir); err == nil || err.Error() != "nope" {
		t.Fatalf("a failing command = %v", err)
	}
}
