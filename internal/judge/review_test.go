package judge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kayra-ML/rove/internal/types"
)

func goalWith(criteria []string, gates ...types.QualityGate) types.Goal {
	return types.Goal{ID: "g", Title: "README yaz", Iteration: 1, CompletionContract: types.CompletionContract{Criteria: criteria, QualityGates: gates, MaxIterations: 5}}
}

// The agent's words never satisfy a criterion: without a reviewer only hard
// evidence counts, so repeating the criterion does nothing.
func TestClaimsAloneNeverFinish(t *testing.T) {
	e := New(nil)
	v := e.Judge(context.Background(), goalWith([]string{"kurulum adımları var"}), Evidence{AgentClaimedDone: true, Summary: "Bitti: kurulum adımları var"}, t.TempDir())
	if v.Decision != types.JudgeCONTINUE {
		t.Fatalf("got %s (%s)", v.Decision, v.Reason)
	}
}

func TestModelReviewDecides(t *testing.T) {
	var seen []string
	e := New(nil)
	e.Ask = func(_ context.Context, agentID types.ID, system, user string) (string, error) {
		seen = append(seen, user)
		if agentID != "a1" || !strings.Contains(system, "JSON only") {
			t.Fatalf("asked %s with %q", agentID, system)
		}
		if strings.Contains(user, "README.md") {
			return "Sure: ```json\n{\"met\":[1,2],\"missing\":[],\"reason\":\"both shown in the diff\"}\n```", nil
		}
		return `{"met":[1],"missing":[{"n":2,"why":"no tests"}],"reason":"half done"}`, nil
	}
	g := goalWith([]string{"README var", "testler geçer"})
	v := e.Judge(context.Background(), g, Evidence{AgentID: "a1", Summary: "yaptım"}, t.TempDir())
	if v.Decision != types.JudgeCONTINUE || !strings.Contains(v.Reason, "testler geçer") || !strings.Contains(v.Reason, "criterion 2: no tests") {
		t.Fatalf("got %s (%s)", v.Decision, v.Reason)
	}
	v = e.Judge(context.Background(), g, Evidence{AgentID: "a1", DiffStat: "README.md | 3 +++", Diff: "+# Kurulum"}, t.TempDir())
	if v.Decision != types.JudgeDONE {
		t.Fatalf("got %s (%s)", v.Decision, v.Reason)
	}
	if !strings.Contains(seen[1], "Diff:\n+# Kurulum") || !strings.Contains(seen[1], "1. README var") {
		t.Fatalf("reviewer input:\n%s", seen[1])
	}
	// an unreadable verdict meets nothing
	e.Ask = func(context.Context, types.ID, string, string) (string, error) { return "looks fine to me", nil }
	if v := e.Judge(context.Background(), g, Evidence{AgentID: "a1", DiffStat: "README.md"}, t.TempDir()); v.Decision != types.JudgeCONTINUE {
		t.Fatalf("unreadable verdict gave %s", v.Decision)
	}
}

func TestDoubleReviewAndRequiredChanges(t *testing.T) {
	e := New(nil)
	e.Ask = func(_ context.Context, _ types.ID, system, _ string) (string, error) {
		if strings.Contains(system, "sceptical") {
			return `{"met":[],"missing":[{"n":1,"why":"not proven"}],"reason":"unconvinced"}`, nil
		}
		return `{"met":[1],"missing":[],"reason":"fine"}`, nil
	}
	g := goalWith([]string{"x"})
	if v := e.Judge(context.Background(), g, Evidence{AgentID: "a", DiffStat: "a.go", Reviewers: 1}, t.TempDir()); v.Decision != types.JudgeDONE {
		t.Fatalf("one reviewer: %s %s", v.Decision, v.Reason)
	}
	if v := e.Judge(context.Background(), g, Evidence{AgentID: "a", DiffStat: "a.go", Reviewers: 2}, t.TempDir()); v.Decision != types.JudgeCONTINUE {
		t.Fatalf("two reviewers must agree: %s", v.Decision)
	}
	if v := e.Judge(context.Background(), g, Evidence{AgentID: "a", RequireChanges: true}, t.TempDir()); v.Decision != types.JudgeCONTINUE || !strings.Contains(v.Reason, "no changes") {
		t.Fatalf("done without changes: %s %s", v.Decision, v.Reason)
	}
}

// Gates run once per iteration: the engine's results are used as they are,
// and gates left to the judge run only when the criteria are met.
func TestGatesRunOnce(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "runs")
	gate := types.QualityGate{Name: "tests", Kind: types.GateTest, Command: "echo x >> " + count, ExpectExitZero: true}
	runs := func() int {
		b, _ := os.ReadFile(count)
		return strings.Count(string(b), "x")
	}
	e := New(nil)
	met := true
	e.Ask = func(context.Context, types.ID, string, string) (string, error) {
		if met {
			return `{"met":[1],"missing":[],"reason":"ok"}`, nil
		}
		return `{"met":[],"missing":[{"n":1,"why":"no"}],"reason":"no"}`, nil
	}
	g := goalWith([]string{"x"}, gate)
	pre := e.RunGates(context.Background(), g.CompletionContract.QualityGates, dir)
	if v := e.Judge(context.Background(), g, Evidence{AgentID: "a", GateResults: pre, GatesRun: true}, dir); v.Decision != types.JudgeDONE || runs() != 1 {
		t.Fatalf("engine-run gates: %s, ran %d times", v.Decision, runs())
	}
	met = false
	if v := e.Judge(context.Background(), g, Evidence{AgentID: "a"}, dir); v.Decision != types.JudgeCONTINUE || runs() != 1 {
		t.Fatalf("gates ran for unmet criteria (%d runs)", runs())
	}
	met = true
	if v := e.Judge(context.Background(), g, Evidence{AgentID: "a"}, dir); v.Decision != types.JudgeDONE || runs() != 2 {
		t.Fatalf("deferred gates: %s, %d runs", v.Decision, runs())
	}
}
