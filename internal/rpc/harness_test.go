package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/harness"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// A goal's harness can be read, picked by hand, given back to the
// composer, and its changes followed; a running goal can be stopped, and
// its profile is not changed under it.
func TestHarnessThroughRPC(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	app.Router.Register("goalm", &goalModel{})
	ag, _ := app.Agents.Upsert(ctx, types.Agent{Name: "g", Provider: "goalm", Model: "m"})
	call := func(method string, params, out any) string {
		t.Helper()
		raw, _ := json.Marshal(params)
		resp := s.Dispatch(ctx, protocol.Request{Method: method, Token: app.Token, Params: raw})
		if !resp.OK {
			return resp.Error
		}
		if out != nil {
			_ = json.Unmarshal(resp.Result, out)
		}
		return ""
	}
	must := func(method string, params, out any) {
		t.Helper()
		if e := call(method, params, out); e != "" {
			t.Fatalf("%s: %s", method, e)
		}
	}

	var cats []harness.Category
	must(protocol.MethodHarnessCatalog, nil, &cats)
	if len(cats) != 5 || cats[0].Key != "context" || cats[4].Key != "recovery" {
		t.Fatalf("catalog = %+v", cats)
	}

	var chat types.Session
	must(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &chat)
	var g types.Goal
	must(protocol.MethodGoalCreate, map[string]any{"title": "yavaş", "sessionId": chat.ID, "completionContract": map[string]any{"criteria": []string{"x"}}}, &g)

	var v harnessView
	must(protocol.MethodHarnessGet, map[string]any{"goalId": g.ID}, &v)
	if v.Profile.Mode != harness.ProfileAuto || v.Resolved.MaxTurns == 0 || v.Running || len(v.Mutations) != 0 {
		t.Fatalf("composed = %+v", v)
	}

	// by hand: retired flags are dropped, the change is in the trace
	manual := harness.GoalExecProfile{
		Context: harness.RepositoryMap, Execution: harness.PlanExecute | harness.Parallel,
		Tools: harness.CodingTools | harness.SequentialTools, Verify: harness.TestVerify, Recovery: harness.Retry,
	}
	must(protocol.MethodHarnessSet, map[string]any{"goalId": g.ID, "profile": manual}, &v)
	if v.Profile.Mode != harness.ProfileManual || v.Profile.Execution != harness.PlanExecute || v.Profile.Version != 1 {
		t.Fatalf("set = %+v", v.Profile)
	}
	if len(v.Mutations) != 1 || v.Mutations[0].Reason != "set by hand" || v.Mutations[0].Old.Mode != harness.ProfileAuto {
		t.Fatalf("trace = %+v", v.Mutations)
	}
	if !strings.Contains(v.Resolved.Tools, "coding tools") || v.Resolved.ToolConcurrency != 1 {
		t.Fatalf("resolved = %+v", v.Resolved)
	}
	bad := manual
	bad.Context = 1 << 12
	if e := call(protocol.MethodHarnessSet, map[string]any{"goalId": g.ID, "profile": bad}, nil); !strings.Contains(e, "unknown harness tactics") {
		t.Fatalf("unknown flag: %q", e)
	}

	must(protocol.MethodHarnessCompose, map[string]any{"goalId": g.ID}, &v)
	if v.Profile.Mode != harness.ProfileAuto || v.Profile.Version != 2 || len(v.Mutations) != 2 {
		t.Fatalf("composed again = %+v (%d mutations)", v.Profile, len(v.Mutations))
	}
	var muts []mutationView
	must(protocol.MethodHarnessMutations, map[string]any{"goalId": g.ID}, &muts)
	if len(muts) != 2 || muts[1].Reason != "composed again" || muts[1].Old.Mode != harness.ProfileManual {
		t.Fatalf("mutations = %+v", muts)
	}

	// running: its profile stays as it is, and it can be stopped
	var stopped struct{ Stopped bool }
	must(protocol.MethodGoalCancel, map[string]any{"id": g.ID}, &stopped)
	if stopped.Stopped {
		t.Fatal("stopped a goal that was not running")
	}
	must(protocol.MethodGoalDrive, map[string]any{"id": g.ID, "sessionId": chat.ID}, nil)
	for end := time.Now().Add(5 * time.Second); !app.Goals.Running(g.ID); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("goal never ran")
		}
	}
	if e := call(protocol.MethodHarnessSet, map[string]any{"goalId": g.ID, "profile": manual}, nil); !strings.Contains(e, "already running") {
		t.Fatalf("set while running: %q", e)
	}
	must(protocol.MethodHarnessGet, map[string]any{"goalId": g.ID}, &v)
	if !v.Running {
		t.Fatal("harness.get does not say the goal runs")
	}
	must(protocol.MethodGoalCancel, map[string]any{"id": g.ID}, &stopped)
	if !stopped.Stopped {
		t.Fatal("goal.cancel did not stop the running goal")
	}
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var cur types.Goal
		must(protocol.MethodGoalGet, map[string]any{"id": g.ID}, &cur)
		if cur.Status == types.GoalCanceled {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("goal is %s after goal.cancel", cur.Status)
		}
	}
	var verdicts []types.JudgeVerdict
	must(protocol.MethodGoalVerdicts, map[string]any{"id": g.ID}, &verdicts)
	if verdicts == nil {
		t.Fatal("verdicts is null, want a list")
	}

	// a deleted goal takes its trace with it
	must(protocol.MethodGoalDelete, map[string]any{"id": g.ID}, nil)
	if rows, _ := app.Store.ListHarnessMutations(ctx, g.ID); len(rows) != 0 {
		t.Fatalf("%d mutations left after delete", len(rows))
	}
}
