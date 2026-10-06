package team

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

func setup(t *testing.T) (*Team, *store.Store, *session.Manager, types.Session) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sm := session.New(st, nil)
	s, err := sm.Create(context.Background(), "", "ag", "")
	if err != nil {
		t.Fatal(err)
	}
	return New(st, sm), st, sm, s
}

// A chat hands work out; a channel — a subagent's own — does not.
func TestBriefIsForTheChatNotItsChannels(t *testing.T) {
	tm, _, sm, s := setup(t)
	ctx := context.Background()
	if b := tm.Brief(ctx, s.ID); !strings.Contains(b, "team_delegate") {
		t.Fatalf("brief = %q", b)
	}
	k, err := sm.CreateChildIn(ctx, s, types.SpaceWorker, "Subagent · x")
	if err != nil {
		t.Fatal(err)
	}
	if b := tm.Brief(ctx, k.ID); b != "" {
		t.Fatalf("a subagent's channel was told to delegate: %q", b)
	}
}

// Orchestra splits the work between plain subagents: each task gets a
// channel of its own and no character, the tasks of one call run side by
// side, and only clipped reports come back.
func TestDelegateRunsSubagentsInParallelAndClips(t *testing.T) {
	tm, st, sm, s := setup(t)
	ctx := context.Background()
	var inFlight, peak int32
	gotCh := make(chan agent.RunRequest, 4)
	d := Delegate{&Subagents{Team: tm, Run: func(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(60 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		gotCh <- req
		return agent.RunResult{Assistant: strings.Repeat("ok ", 1000), FilesEdited: []string{"a.ts", "a.ts"}, Done: true}, nil
	}}}
	// a member named by an older lead is ignored: the task still runs
	args, _ := json.Marshal(map[string]any{"assignments": []map[string]string{
		{"member": "frontend", "task": "build the login form"},
		{"task": "add the /login route"},
	}})
	res, err := d.Call(ctx, tool.Context{SessionID: s.ID}, args)
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	close(gotCh)
	if peak < 2 {
		t.Fatalf("subagents ran one after another (peak %d)", peak)
	}
	kids, _ := sm.Children(ctx, s.ID)
	if len(kids) != 2 {
		t.Fatalf("channels = %+v", kids)
	}
	for r := range gotCh {
		if r.HistoryLimit != 0 || !strings.HasPrefix(r.UserMessage, "Task from the lead:") || r.MaxTurns != childTurns {
			t.Fatalf("%+v", r)
		}
		if sp, err := st.GetSessionPersona(ctx, r.SessionID); err == nil && (sp.CharacterID != "" || sp.ProfileID != "") {
			t.Fatalf("a subagent got a persona: %+v", sp)
		}
	}
	if !strings.Contains(res.Content, "TASK 1/2 · subagent · completed") || !strings.Contains(res.Content, "Files: a.ts\n") {
		t.Fatalf("report = %q", res.Content[:200])
	}
	if len([]rune(res.Content)) > 2*(reportClip+200) {
		t.Fatalf("reports not clipped: %d runes", len([]rune(res.Content)))
	}
}

// A lead that splits a job into many parts must not start them all at
// once: the bill and the provider's rate limit would both arrive together,
// and nothing about the job is faster for it.
func TestDelegateRunsAtMostFourAtOnce(t *testing.T) {
	ctx := context.Background()
	tm, _, _, s := setup(t)
	var now, peak int64
	d := Delegate{&Subagents{Team: tm, Run: func(context.Context, agent.RunRequest) (agent.RunResult, error) {
		n := atomic.AddInt64(&now, 1)
		for {
			p := atomic.LoadInt64(&peak)
			if n <= p || atomic.CompareAndSwapInt64(&peak, p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt64(&now, -1)
		return agent.RunResult{Assistant: "done", Done: true}, nil
	}}}
	var as []map[string]string
	for i := 0; i < 6; i++ {
		as = append(as, map[string]string{"goal": "go"})
	}
	args, _ := json.Marshal(map[string]any{"tasks": as})
	res, err := d.Call(ctx, tool.Context{SessionID: s.ID}, args)
	if err != nil || res.IsError {
		t.Fatalf("call: %v %q", err, res.Content)
	}
	if peak > maxParallel {
		t.Fatalf("ran %d at once, cap is %d", peak, maxParallel)
	}
	if strings.Count(res.Content, "TASK ") != 6 {
		t.Fatalf("every subagent should still report: %q", res.Content)
	}
}

func TestDelegateRefusesAFloodOfAssignments(t *testing.T) {
	ctx := context.Background()
	tm, _, _, s := setup(t)
	var as []map[string]string
	for i := 0; i < maxAssignments+1; i++ {
		as = append(as, map[string]string{"goal": "go"})
	}
	args, _ := json.Marshal(map[string]any{"tasks": as})
	ran := int64(0)
	d := Delegate{&Subagents{Team: tm, Run: func(context.Context, agent.RunRequest) (agent.RunResult, error) {
		atomic.AddInt64(&ran, 1)
		return agent.RunResult{}, nil
	}}}
	res, _ := d.Call(ctx, tool.Context{SessionID: s.ID}, args)
	if !res.IsError || !strings.Contains(res.Content, "too many assignments") {
		t.Fatalf("expected a refusal, got %q", res.Content)
	}
	if ran != 0 {
		t.Fatalf("nothing should have run, %d did", ran)
	}
}
