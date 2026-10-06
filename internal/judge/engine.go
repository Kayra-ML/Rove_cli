package judge

import (
	"context"
	"encoding/json"
	"fmt"
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

	// AgentID is whose model reviews (a tool-less call, fresh context).
	AgentID types.ID
	// DiffStat lists what changed in the workspace; Diff is the (clipped)
	// diff itself, sent only when the reviewer is to read it.
	DiffStat    string
	Diff        string
	FilesEdited []string
	// GateResults the engine already ran this iteration (GatesRun). When the
	// engine did not run them, the judge runs them — but only once the
	// criteria are met, so a failing iteration does not pay for them.
	GateResults []types.GateResult
	GatesRun    bool
	// Reviewers: 2 asks two independent reviewers who must both agree.
	Reviewers int
	// RequireChanges: done needs changed files in the workspace.
	RequireChanges bool
}

// Asker is one tool-less model call: the reviewer.
type Asker func(ctx context.Context, agentID types.ID, system, user string) (string, error)

type Engine struct {
	gates *qualitygate.Runner
	// Ask, when set, reviews the criteria with a model. Without it (tests,
	// offline) a criterion counts only on hard evidence: a hit, an
	// artifact, or — when the contract has gates — all gates passing.
	Ask Asker
}

func New(g *qualitygate.Runner) *Engine {
	if g == nil {
		g = qualitygate.New()
	}
	return &Engine{gates: g}
}

// review budgets: what the reviewer reads, clipped
const (
	reviewSummary  = 2000
	reviewDiffStat = 1500
	reviewDiff     = 6000
)

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
	gates := goal.CompletionContract.QualityGates
	if ev.GatesRun {
		v.GateResults = ev.GateResults
	}

	missing, why := e.missing(ctx, goal, ev)
	// gates the engine left to us run once the criteria are met
	if !ev.GatesRun && len(missing) == 0 && len(gates) > 0 {
		v.GateResults = e.gates.Run(ctx, gates, workDir)
	}
	gatesOK := qualitygate.AllPassed(v.GateResults)
	if len(gates) > 0 && len(v.GateResults) == 0 {
		gatesOK = false // not run yet
	}
	changed := len(ev.FilesEdited) > 0 || strings.TrimSpace(ev.DiffStat) != ""

	if len(missing) == 0 && gatesOK && (!ev.RequireChanges || changed) {
		v.Decision = types.JudgeDONE
		v.Reason = "completion contract satisfied"
		if len(gates) > 0 {
			v.Reason += "; quality gates passed"
		}
		if why != "" {
			v.Reason += " (" + why + ")"
		}
		return v
	}

	v.Decision = types.JudgeCONTINUE
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "unmet criteria: "+strings.Join(missing, "; "))
	}
	if len(missing) == 0 && !gatesOK {
		parts = append(parts, "quality gates failed")
		for _, g := range v.GateResults {
			if !g.Passed {
				parts = append(parts, g.Name+" exit="+itoa(g.ExitCode))
			}
		}
	}
	if len(missing) == 0 && gatesOK && ev.RequireChanges && !changed {
		parts = append(parts, "no changes in the workspace")
	}
	if why != "" {
		parts = append(parts, why)
	}
	if ev.AgentClaimedDone {
		parts = append(parts, "agent claim of completion is insufficient")
	}
	v.Reason = strings.Join(parts, ". ")
	return v
}

// missing returns the criteria the evidence does not show as met, and the
// reviewer's one-line reason.
func (e *Engine) missing(ctx context.Context, goal types.Goal, ev Evidence) ([]string, string) {
	criteria := goal.CompletionContract.Criteria
	if len(criteria) == 0 {
		return nil, ""
	}
	if e.Ask == nil || ev.AgentID == "" {
		return missingOffline(goal, ev), ""
	}
	n := ev.Reviewers
	if n < 1 {
		n = 1
	}
	met := make([]bool, len(criteria))
	for i := range met {
		met[i] = true
	}
	var why []string
	for r := 0; r < n; r++ {
		got, reason, err := e.review(ctx, goal, ev, r)
		if err != nil {
			return append([]string{}, criteria...), "review failed: " + err.Error()
		}
		for i := range met {
			met[i] = met[i] && got[i]
		}
		if reason != "" {
			why = append(why, reason)
		}
	}
	var out []string
	for i, c := range criteria {
		if !met[i] && !ev.CriteriaHits[c] {
			out = append(out, c)
		}
	}
	return out, strings.Join(why, " / ")
}

var reviewerPrompts = []string{
	`You review an AI coding agent's work against a goal's completion criteria. Decide for each criterion whether the evidence shows it is met. The agent's own report is a claim, not proof: count a criterion as met only when the changes or the check results support it.`,
	`You are a second, sceptical reviewer of an AI coding agent's work. Assume each completion criterion is NOT met until the changes or the check results clearly show it is. The agent's report alone never proves anything.`,
}

const reviewReply = `
Reply with JSON only: {"met": [criterion numbers], "missing": [{"n": number, "why": "short"}], "reason": "one short sentence"}`

// review asks one reviewer; met is indexed like the criteria.
func (e *Engine) review(ctx context.Context, goal types.Goal, ev Evidence, which int) ([]bool, string, error) {
	criteria := goal.CompletionContract.Criteria
	var b strings.Builder
	b.WriteString("Goal: " + goal.Title + "\n")
	if d := strings.TrimSpace(goal.Description); d != "" {
		b.WriteString(clip(d, 600) + "\n")
	}
	b.WriteString("\nCompletion criteria:\n")
	for i, c := range criteria {
		b.WriteString(itoa(i+1) + ". " + c + "\n")
	}
	b.WriteString("\nAgent's report:\n" + orNone(clip(ev.Summary, reviewSummary)) + "\n")
	b.WriteString("\nChanged files:\n" + orNone(clip(ev.DiffStat, reviewDiffStat)) + "\n")
	if ev.DiffStat == "" && len(ev.FilesEdited) > 0 {
		b.WriteString(clip(strings.Join(ev.FilesEdited, "\n"), reviewDiffStat) + "\n")
	}
	if ev.Diff != "" {
		b.WriteString("\nDiff:\n" + clip(ev.Diff, reviewDiff) + "\n")
	}
	if len(ev.GateResults) > 0 {
		b.WriteString("\nChecks:\n")
		for _, g := range ev.GateResults {
			state := "passed"
			if !g.Passed {
				state = "failed, exit " + itoa(g.ExitCode)
			}
			b.WriteString("- " + g.Name + ": " + state + "\n")
		}
	}
	reply, err := e.Ask(ctx, ev.AgentID, reviewerPrompts[which%len(reviewerPrompts)]+reviewReply, b.String())
	if err != nil {
		return nil, "", err
	}
	return parseReview(reply, len(criteria))
}

type reviewJSON struct {
	Met     []int `json:"met"`
	Missing []struct {
		N   int    `json:"n"`
		Why string `json:"why"`
	} `json:"missing"`
	Reason string `json:"reason"`
}

// parseReview reads the reviewer's JSON (the first object in the reply). A
// criterion is met only when listed as met and not also as missing; a reply
// that cannot be read meets nothing.
func parseReview(reply string, n int) ([]bool, string, error) {
	met := make([]bool, n)
	start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if start < 0 || end <= start {
		return met, "", fmt.Errorf("the reviewer gave no verdict")
	}
	var r reviewJSON
	if err := json.Unmarshal([]byte(reply[start:end+1]), &r); err != nil {
		return met, "", fmt.Errorf("the reviewer's verdict is not valid JSON")
	}
	for _, i := range r.Met {
		if i >= 1 && i <= n {
			met[i-1] = true
		}
	}
	var whys []string
	for _, m := range r.Missing {
		if m.N >= 1 && m.N <= n {
			met[m.N-1] = false
			if m.Why != "" {
				whys = append(whys, itoa(m.N)+": "+clip(m.Why, 160))
			}
		}
	}
	reason := clip(strings.TrimSpace(r.Reason), 240)
	if len(whys) > 0 {
		detail := "criterion " + strings.Join(whys, "; criterion ")
		if reason == "" {
			reason = detail
		} else {
			reason += " (" + detail + ")"
		}
	}
	return met, reason, nil
}

// missingOffline is the no-reviewer rule: hard evidence only. The agent's
// words never count — a criterion is met by an explicit hit, a matching
// artifact, or (for a contract with gates) every gate passing.
func missingOffline(goal types.Goal, ev Evidence) []string {
	gates := goal.CompletionContract.QualityGates
	gatesPass := len(gates) > 0 && ev.GatesRun && len(ev.GateResults) > 0 && qualitygate.AllPassed(ev.GateResults)
	var missing []string
	for _, c := range goal.CompletionContract.Criteria {
		if ev.CriteriaHits[c] || artifactNamed(ev.Artifacts, c) || gatesPass {
			continue
		}
		missing = append(missing, c)
	}
	return missing
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
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
