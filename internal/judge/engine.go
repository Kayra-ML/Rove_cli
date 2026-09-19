package judge

import (
	"context"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/qualitygate"
	"github.com/Kayra-ML/rove/internal/types"
)

type Evidence struct {
	AgentClaimedDone bool
	Summary          string
	Artifacts        []types.Artifact
	CriteriaHits     map[string]bool
}

type Engine struct {
	gates *qualitygate.Runner
}

func New(g *qualitygate.Runner) *Engine {
	if g == nil {
		g = qualitygate.New()
	}
	return &Engine{gates: g}
}

func (e *Engine) Judge(ctx context.Context, goal types.Goal, ev Evidence, workDir string) types.JudgeVerdict {
	v := types.JudgeVerdict{
		ID:        id.NewID(),
		GoalID:    goal.ID,
		Iteration: goal.Iteration,
		CreatedAt: time.Now().UTC(),
	}
	if goal.CompletionContract.MaxIterations > 0 && goal.Iteration > goal.CompletionContract.MaxIterations {
		v.Decision = types.JudgeBLOCKED
		v.Reason = "max iterations reached without satisfying the completion contract"
		return v
	}

	missing := missingCriteria(goal.CompletionContract.Criteria, ev)
	if len(goal.CompletionContract.QualityGates) > 0 {
		v.GateResults = e.gates.Run(ctx, goal.CompletionContract.QualityGates, workDir)
	}
	gatesOK := qualitygate.AllPassed(v.GateResults)

	if len(missing) == 0 && gatesOK {
		v.Decision = types.JudgeDONE
		v.Reason = "completion contract satisfied; quality gates passed"
		return v
	}

	if ev.AgentClaimedDone && (len(missing) > 0 || !gatesOK) {
		v.Decision = types.JudgeCONTINUE
		var parts []string
		if len(missing) > 0 {
			parts = append(parts, "unmet criteria: "+strings.Join(missing, "; "))
		}
		if !gatesOK {
			parts = append(parts, "quality gates failed")
			for _, g := range v.GateResults {
				if !g.Passed {
					parts = append(parts, g.Name+" exit="+itoa(g.ExitCode))
				}
			}
		}
		parts = append(parts, "agent claim of completion is insufficient")
		v.Reason = strings.Join(parts, ". ")
		return v
	}

	if len(missing) > 0 || !gatesOK {
		v.Decision = types.JudgeCONTINUE
		var parts []string
		if len(missing) > 0 {
			parts = append(parts, "unmet criteria: "+strings.Join(missing, "; "))
		}
		if !gatesOK {
			parts = append(parts, "quality gates failed")
		}
		v.Reason = strings.Join(parts, ". ")
		return v
	}

	v.Decision = types.JudgeDONE
	v.Reason = "ok"
	return v
}

func missingCriteria(criteria []string, ev Evidence) []string {
	var missing []string
	summary := strings.ToLower(ev.Summary)
	for _, c := range criteria {
		if ev.CriteriaHits != nil {
			if ev.CriteriaHits[c] {
				continue
			}
		}
		if c != "" && strings.Contains(summary, strings.ToLower(c)) {
			continue
		}
		if artifactNamed(ev.Artifacts, c) {
			continue
		}
		missing = append(missing, c)
	}
	return missing
}

func artifactNamed(arts []types.Artifact, c string) bool {
	cl := strings.ToLower(c)
	for _, a := range arts {
		if strings.Contains(strings.ToLower(a.Name), cl) || strings.Contains(strings.ToLower(a.Path), cl) {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// RunGates executes quality gates and returns results without a full judge verdict.
// Used by the goal engine when it wants to check gates separately from the judge decision.
func (e *Engine) RunGates(ctx context.Context, gates []types.QualityGate, workDir string) []types.GateResult {
	return e.gates.Run(ctx, gates, workDir)
}
