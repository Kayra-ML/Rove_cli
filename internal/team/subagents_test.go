package team

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/gitwt"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

func call(t *testing.T, d Delegate, sid types.ID, ws string, tasks ...map[string]string) tool.Result {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"tasks": tasks})
	res, err := d.Call(context.Background(), tool.Context{SessionID: sid, Workspace: ws}, args)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// Like Hermes' delegate_task: the subagent gets a channel of its own,
// starts with nothing but its task, and is told it is a subagent.
func TestSubagentStartsFresh(t *testing.T) {
	tm, _, sm, s := setup(t)
	var got agent.RunRequest
	d := Delegate{&Subagents{Team: tm, Run: func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
		got = req
		return agent.RunResult{Assistant: "found three callers", Done: true, Turns: 3}, nil
	}}}
	res := call(t, d, s.ID, "", map[string]string{"goal": "find who calls Parse", "context": "look in /repo/internal"})
	if res.IsError || !strings.Contains(res.Content, "TASK 1/1 · subagent · completed · 3 turns\nfound three callers") {
		t.Fatalf("report = %q", res.Content)
	}
	if got.HistoryLimit != 0 || !strings.Contains(got.UserMessage, "find who calls Parse") ||
		!strings.Contains(got.UserMessage, "Context:\nlook in /repo/internal") {
		t.Fatalf("brief = %+v", got)
	}
	if !strings.Contains(got.SystemExtra, "You are a subagent") || strings.Contains(got.SystemExtra, "own copy of the project") {
		t.Fatalf("rules = %q", got.SystemExtra)
	}
	kids, _ := sm.Children(context.Background(), s.ID)
	if len(kids) != 1 || kids[0].Space != types.SpaceWorker || got.SessionID != kids[0].ID {
		t.Fatalf("channel = %+v, ran in %s", kids, got.SessionID)
	}
	list := d.List(context.Background(), s.ID)
	if len(list) != 1 || list[0].Status != StatusCompleted || list[0].Summary != "found three callers" || list[0].EndedAt.IsZero() {
		t.Fatalf("roster = %+v", list)
	}
}

// What went wrong is kept apart from what merely ran out of road.
func TestReportSaysHowEachTaskEnded(t *testing.T) {
	tm, _, _, s := setup(t)
	n := int32(0)
	d := Delegate{&Subagents{Team: tm, Run: func(context.Context, agent.RunRequest) (agent.RunResult, error) {
		switch atomic.AddInt32(&n, 1) {
		case 1:
			return agent.RunResult{Assistant: "half", Turns: childTurns}, nil
		default:
			return agent.RunResult{}, errText("model unavailable")
		}
	}}}
	// one at a time, so which is which is certain
	res := call(t, d, s.ID, "", map[string]string{"goal": "a"})
	if !strings.Contains(res.Content, "max_turns") || !strings.Contains(res.Content, "the work is partial") {
		t.Fatalf("cut short = %q", res.Content)
	}
	res = call(t, d, s.ID, "", map[string]string{"goal": "b"})
	if !res.IsError || !strings.Contains(res.Content, "failed: model unavailable") {
		t.Fatalf("failed = %q", res.Content)
	}
}

type errText string

func (e errText) Error() string { return string(e) }

// A task still waiting for a free slot can be called off before it costs
// anything; a running one is cancelled; either way it says "stopped".
func TestStoppingSubagents(t *testing.T) {
	tm, _, _, s := setup(t)
	release := make(chan struct{})
	var started int32
	var cancelled []types.ID
	var mu sync.Mutex
	subs := &Subagents{Team: tm,
		Run: func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error) {
			atomic.AddInt32(&started, 1)
			select {
			case <-release:
				return agent.RunResult{Done: true}, nil
			case <-ctx.Done():
				return agent.RunResult{}, ctx.Err()
			}
		},
		Cancel: func(id types.ID) { mu.Lock(); cancelled = append(cancelled, id); mu.Unlock() },
	}
	d := Delegate{subs}
	var tasks []map[string]string
	for i := 0; i < maxParallel+1; i++ {
		tasks = append(tasks, map[string]string{"goal": "work"})
	}
	done := make(chan tool.Result)
	go func() { done <- call(t, d, s.ID, "", tasks...) }()

	var list []Task
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		list = subs.List(context.Background(), s.ID)
		if atomic.LoadInt32(&started) == maxParallel && len(list) == maxParallel+1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var waiting, running Task
	for _, x := range list {
		switch x.Status {
		case StatusQueued:
			waiting = x
		case StatusRunning:
			running = x
		}
	}
	if waiting.ID == "" || running.ID == "" {
		t.Fatalf("expected one waiting and some running: %+v", list)
	}
	if got, _ := subs.Stop(waiting.ID); got.Status != StatusInterrupted {
		t.Fatalf("waiting task after stop = %s", got.Status)
	}
	if got, _ := subs.Stop(running.ID); got.Status != StatusInterrupted {
		t.Fatalf("running task after stop = %s", got.Status)
	}
	mu.Lock()
	if len(cancelled) != 1 || cancelled[0] != running.SessionID {
		t.Fatalf("cancelled %v, want only %s", cancelled, running.SessionID)
	}
	mu.Unlock()
	close(release)
	res := <-done
	if atomic.LoadInt32(&started) != maxParallel {
		t.Fatalf("the stopped waiting task still ran (%d runs)", started)
	}
	if strings.Count(res.Content, "· interrupted") != 2 {
		t.Fatalf("report = %q", res.Content)
	}
}

func TestSteerReachesOnlyARunningTask(t *testing.T) {
	tm, _, _, s := setup(t)
	release := make(chan struct{})
	var steered string
	subs := &Subagents{Team: tm,
		Run: func(context.Context, agent.RunRequest) (agent.RunResult, error) {
			<-release
			return agent.RunResult{Done: true}, nil
		},
		Steer: func(_ types.ID, text string) bool { steered = text; return true },
	}
	done := make(chan struct{})
	go func() { call(t, Delegate{subs}, s.ID, "", map[string]string{"goal": "g"}); close(done) }()
	var task Task
	for i := 0; i < 400 && task.Status != StatusRunning; i++ {
		if l := subs.List(context.Background(), s.ID); len(l) == 1 {
			task = l[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := subs.SteerTask(task.ID, "use the v2 API instead"); err != nil || steered != "use the v2 API instead" {
		t.Fatalf("steer: %v, got %q", err, steered)
	}
	close(release)
	<-done
	if err := subs.SteerTask(task.ID, "too late"); err == nil {
		t.Fatal("a finished task accepted a steer")
	}
}

// ── checkouts of their own (git) ───────────────────────────────────────────────

func gitRepo(t *testing.T) string {
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
	writeFile(t, repo, "shared.txt", "one\ntwo\nthree\n")
	if err := g.CommitAll(repo, "init"); err != nil {
		t.Fatal(err)
	}
	return repo
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

// The Orca half: parallel subagents each write their own checkout, started
// from the user's tree as it is — uncommitted work included — and what they
// changed lands back in the user's tree, uncommitted, with nothing of the
// checkouts left behind.
func TestParallelSubagentsWorkApartAndLandTogether(t *testing.T) {
	tm, _, _, s := setup(t)
	repo := gitRepo(t)
	writeFile(t, repo, "draft.txt", "not committed yet\n")
	root := t.TempDir()
	var sawDraft int32
	var mu sync.Mutex
	briefs := map[string]string{}
	subs := &Subagents{Team: tm, Git: gitwt.New(), WorktreeRoot: root,
		Run: func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
			if req.Workspace == repo || !strings.HasPrefix(req.Workspace, root) {
				t.Errorf("ran in %s, not its own checkout", req.Workspace)
			}
			if readFile(req.Workspace, "draft.txt") == "not committed yet\n" {
				atomic.AddInt32(&sawDraft, 1)
			}
			mu.Lock()
			briefs[req.Workspace] = req.UserMessage
			mu.Unlock()
			name := "a.go"
			if strings.Contains(req.UserMessage, "second") {
				name = "b.go"
			}
			writeFile(t, req.Workspace, name, "package x\n")
			return agent.RunResult{Assistant: "wrote " + name, Done: true}, nil
		}}
	res := call(t, Delegate{subs}, s.ID, repo,
		map[string]string{"goal": "first: write " + repo + "/a.go"},
		map[string]string{"goal": "second: write " + repo + "/b.go"})
	if res.IsError {
		t.Fatal(res.Content)
	}
	if sawDraft != 2 {
		t.Fatal("a checkout did not carry the user's uncommitted work")
	}
	for ws, b := range briefs {
		if strings.Contains(b, repo+"/") || !strings.Contains(b, ws+"/") {
			t.Fatalf("the lead's paths were not moved into the checkout: %q", b)
		}
	}
	if readFile(repo, "a.go") != "package x\n" || readFile(repo, "b.go") != "package x\n" {
		t.Fatalf("work did not land: a=%q b=%q", readFile(repo, "a.go"), readFile(repo, "b.go"))
	}
	if !strings.Contains(res.Content, "applied to the workspace") {
		t.Fatalf("report = %q", res.Content)
	}
	if left, _ := os.ReadDir(root); len(left) != 0 {
		t.Fatalf("checkouts left behind: %v", left)
	}
	out, _ := exec.Command("git", "-C", repo, "branch", "--list", "rove/*").Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("branches left behind: %s", out)
	}
	if st, _ := exec.Command("git", "-C", repo, "log", "--oneline").Output(); strings.Count(string(st), "\n") != 1 {
		t.Fatalf("the user's branch was committed to:\n%s", st)
	}
}

// Two subagents changed the same line. The first lands; the second must not
// overwrite it — its work waits in its checkout, the lead is told, and the
// user can take it or throw it away.
func TestAClashWaitsInsteadOfOverwriting(t *testing.T) {
	tm, _, _, s := setup(t)
	repo := gitRepo(t)
	root := t.TempDir()
	var order int32
	subs := &Subagents{Team: tm, Git: gitwt.New(), WorktreeRoot: root,
		Run: func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
			// the second to finish is the one that clashes
			who := "first"
			if strings.Contains(req.UserMessage, "slow") {
				time.Sleep(150 * time.Millisecond)
				who = "second"
			}
			writeFile(t, req.Workspace, "shared.txt", "one\n"+who+"\nthree\n")
			atomic.AddInt32(&order, 1)
			return agent.RunResult{Done: true}, nil
		}}
	res := call(t, Delegate{subs}, s.ID, repo,
		map[string]string{"goal": "fast edit"}, map[string]string{"goal": "slow edit"})
	if readFile(repo, "shared.txt") != "one\nfirst\nthree\n" {
		t.Fatalf("the first change was overwritten: %q", readFile(repo, "shared.txt"))
	}
	if !strings.Contains(res.Content, "NOT applied: clashes") {
		t.Fatalf("the lead was not told: %q", res.Content)
	}
	var clash Task
	for _, x := range subs.List(context.Background(), s.ID) {
		if x.Worktree != nil && !x.Worktree.Applied {
			clash = x
		}
	}
	if clash.ID == "" || clash.Worktree.Path == "" || readFile(clash.Worktree.Path, "shared.txt") != "one\nsecond\nthree\n" {
		t.Fatalf("the clashing work was not kept: %+v", clash.Worktree)
	}
	if clash.Worktree.Reason != ReasonClash {
		t.Fatalf("reason = %q", clash.Worktree.Reason)
	}
	if _, err := subs.Apply(clash.ID); err == nil {
		t.Fatal("a clashing patch was applied")
	}
	got, err := subs.Discard(clash.ID)
	if err != nil || got.Worktree.Reason != ReasonDiscarded || got.Worktree.Path != "" {
		t.Fatalf("discard: %v %+v", err, got.Worktree)
	}
	if _, err := os.Stat(clash.Worktree.Path); !os.IsNotExist(err) {
		t.Fatal("the discarded checkout is still on disk")
	}
}

// A lone task shares the workspace: there is nothing to keep apart, and a
// checkout per single edit would only cost time.
func TestALoneTaskWorksInPlace(t *testing.T) {
	tm, _, _, s := setup(t)
	repo := gitRepo(t)
	var ws string
	subs := &Subagents{Team: tm, Git: gitwt.New(), WorktreeRoot: t.TempDir(),
		Run: func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
			ws = req.Workspace
			return agent.RunResult{Done: true}, nil
		}}
	call(t, Delegate{subs}, s.ID, repo, map[string]string{"goal": "one thing"})
	if ws != repo {
		t.Fatalf("a single task ran in %s", ws)
	}
}
