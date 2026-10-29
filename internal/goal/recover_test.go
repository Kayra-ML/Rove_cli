package goal

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Kayra-ML/rove/internal/harness"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/types"
)

// The daemon stopping under a goal is not the user stopping it: the goal
// stays marked running, and the next start drives it on from its
// checkpoint, in the iteration it was in.
func TestAGoalTheDaemonStopsUnderIsResumed(t *testing.T) {
	var resumedPhase atomic.Bool
	m := &scripted{}
	m.agent = func(ctx context.Context, _ int, req provider.ChatRequest) ([]provider.ChatDelta, error) {
		if strings.Contains(lastUser(req), "Iteration 2 ") && !resumedPhase.Load() {
			<-ctx.Done() // the daemon stops while this iteration works
			return nil, ctx.Err()
		}
		return text("ilk adım tamam"), nil
	}
	m.review = func(string) string {
		if resumedPhase.Load() {
			return `{"met":[1],"missing":[],"reason":"ok"}`
		}
		return `{"met":[],"missing":[{"n":1,"why":"not yet"}],"reason":"not yet"}`
	}
	r := newRig(t, m)
	ctx := context.Background()
	chat, _ := r.sess.CreateIn(ctx, types.SpaceChat, "c", r.ag.ID, "")
	g, _ := r.e.Create(ctx, types.Goal{Title: "uzun iş", SessionID: chat.ID, CompletionContract: types.CompletionContract{Criteria: []string{"x"}}})
	if err := r.e.Start(g.ID, chat.ID, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, c := range r.m.agentCalls() {
			if strings.Contains(lastUser(c), "Iteration 2 ") {
				return true
			}
		}
		return false
	})

	r.e.Close()
	if r.e.Running(g.ID) {
		t.Fatal("Close returned with the drive still going")
	}
	got, _ := r.st.GetGoal(ctx, g.ID)
	if got.Status != types.GoalRunning || got.Iteration != 1 {
		t.Fatalf("after the daemon stopped: %s it=%d", got.Status, got.Iteration)
	}
	if err := r.e.Start(g.ID, chat.ID, ""); err != ErrClosed {
		t.Fatalf("a closed engine started a goal: %v", err)
	}

	// the next start
	resumedPhase.Store(true)
	e2 := New(r.st, r.e.bus, r.e.agents, r.e.judge, nil)
	e2.ToolNames = r.e.ToolNames
	t.Cleanup(e2.Close)
	if ids := e2.Recover(ctx); len(ids) != 1 || ids[0] != g.ID {
		t.Fatalf("resumed %v", ids)
	}
	waitFor(t, func() bool { g, _ := r.st.GetGoal(ctx, g.ID); return g.Status == types.GoalDone })
	calls := r.m.agentCalls()
	first := lastUser(calls[len(calls)-1])
	if !strings.Contains(first, "Resuming. Progress so far:") || !strings.Contains(first, "Iteration 2 of 8") {
		t.Fatalf("the resumed drive was not handed its progress:\n%s", first)
	}
	// it got through an iteration: the count of resumes starts over
	if _, err := r.st.GetKV(ctx, resumesKey(g.ID)); err == nil {
		t.Fatal("resume count kept after an iteration finished")
	}
}

// Recover only drives on what its harness says to, and leaves a goal that
// keeps going down with the daemon for the user.
func TestRecoverRespectsTheHarnessAndGivesUp(t *testing.T) {
	m := &scripted{}
	m.agent = func(context.Context, int, provider.ChatRequest) ([]provider.ChatDelta, error) { return text("ok"), nil }
	m.review = func(string) string { return `{"met":[1],"missing":[],"reason":"ok"}` }
	r := newRig(t, m)
	ctx := context.Background()
	chat, _ := r.sess.CreateIn(ctx, types.SpaceChat, "c", r.ag.ID, "")
	running := func(title string, recovery harness.RecoveryTactic) types.Goal {
		g, _ := r.e.Create(ctx, types.Goal{Title: title, SessionID: chat.ID, CompletionContract: types.CompletionContract{Criteria: []string{"x"}}})
		g.Status = types.GoalRunning
		_ = r.st.UpsertGoal(ctx, g)
		hp := harness.HarnessProfile{GoalID: string(g.ID), Current: harness.GoalExecProfile{Mode: harness.ProfileAuto, Recovery: recovery}}
		raw, _ := json.Marshal(&hp)
		_ = r.st.SaveGoalHarness(ctx, g.ID, string(raw))
		return g
	}
	plain := running("no resume", harness.Retry)
	looping := running("keeps stopping", harness.Retry|harness.CheckpointResume)
	_ = r.st.PutKV(ctx, resumesKey(looping.ID), strconv.Itoa(maxResumes))

	if ids := r.e.Recover(ctx); len(ids) != 0 {
		t.Fatalf("resumed %v", ids)
	}
	if g, _ := r.st.GetGoal(ctx, plain.ID); g.Status != types.GoalPending {
		t.Fatalf("a goal without CheckpointResume is %s", g.Status)
	}
	g, _ := r.st.GetGoal(ctx, looping.ID)
	if g.Status != types.GoalBlocked || g.LastVerdict == nil || !strings.Contains(g.LastVerdict.Reason, "stopped under this goal") {
		t.Fatalf("a goal resumed %d times is %s (%+v)", maxResumes, g.Status, g.LastVerdict)
	}
}
