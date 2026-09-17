package judge

import (
	"context"
	"testing"

	"github.com/aether-dev/aether/internal/types"
)

func TestRejectsAgentClaimWithoutGates(t *testing.T) {
	e := New(nil)
	goal := types.Goal{
		ID: "g",
		CompletionContract: types.CompletionContract{
			Criteria: []string{"tests pass"},
			QualityGates: []types.QualityGate{
				{Name: "tests", Kind: types.GateTest, Command: "exit 1", ExpectExitZero: true},
			},
			MaxIterations: 5,
		},
		Iteration: 1,
	}
	v := e.Judge(context.Background(), goal, Evidence{AgentClaimedDone: true, Summary: "I am done"}, t.TempDir())
	if v.Decision != types.JudgeCONTINUE {
		t.Fatalf("got %s (%s)", v.Decision, v.Reason)
	}
}

func TestDoneWhenCriteriaAndGatesPass(t *testing.T) {
	e := New(nil)
	goal := types.Goal{
		ID: "g",
		CompletionContract: types.CompletionContract{
			Criteria: []string{"hello"},
			QualityGates: []types.QualityGate{
				{Name: "ok", Kind: types.GateBuild, Command: "exit 0", ExpectExitZero: true},
			},
			MaxIterations: 5,
		},
	}
	v := e.Judge(context.Background(), goal, Evidence{Summary: "hello shipped", CriteriaHits: map[string]bool{"hello": true}}, t.TempDir())
	if v.Decision != types.JudgeDONE {
		t.Fatalf("got %s %s", v.Decision, v.Reason)
	}
}

func TestMaxIterationsBlocks(t *testing.T) {
	e := New(nil)
	goal := types.Goal{
		CompletionContract: types.CompletionContract{MaxIterations: 2, Criteria: []string{"x"}},
		Iteration:          3,
	}
	v := e.Judge(context.Background(), goal, Evidence{}, t.TempDir())
	if v.Decision != types.JudgeBLOCKED {
		t.Fatalf("got %s", v.Decision)
	}
}

func TestFirstIterationNotBlockedAtMaxOne(t *testing.T) {
	e := New(nil)
	goal := types.Goal{
		CompletionContract: types.CompletionContract{
			MaxIterations: 1,
			Criteria:      []string{"never-this-token"},
			QualityGates:  []types.QualityGate{{Name: "ok", Kind: types.GateTest, Command: "exit 0", ExpectExitZero: true}},
		},
		Iteration: 1,
	}
	v := e.Judge(context.Background(), goal, Evidence{}, t.TempDir())
	if v.Decision != types.JudgeCONTINUE {
		t.Fatalf("iteration 1 of max 1 should CONTINUE, got %s", v.Decision)
	}
}
