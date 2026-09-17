package harness

import (
	"math"
	"strings"
)

// TaskAnalysis is the input to the HarnessComposer.
// Fields that are zero/empty are treated as unknown.
type TaskAnalysis struct {
	// Classification
	TaskType   string // "bug_fix" | "feature" | "refactor" | "research" | "review" | "test" | "generic"
	Complexity float64 // 0.0 (trivial) … 1.0 (very hard)
	RiskLevel  float64 // 0.0 (safe) … 1.0 (dangerous)

	// Scope
	RepositoryFiles  int     // total files in repo
	AffectedFiles    int     // estimated files the task touches
	DependencyDepth  int     // depth of import/dependency graph
	EstimatedMinutes float64 // rough wall-clock estimate

	// Autonomy & cost
	RequestedAutonomy float64 // 0.0 (supervised) … 1.0 (fully autonomous)
	TokenBudget       int     // 0 = unlimited
	CostBudgetUSD     float64 // 0 = unlimited

	// Available models
	AvailableModels     []string
	PrimaryModelCap     ModelCap
	FallbackModelRef    string

	// Historical harness eval (optional)
	HistoricalSuccessRate float64 // 0.0 … 1.0
	PreviousStuckSignals  []StuckSignal
}

// ModelCap describes a model's context capabilities.
type ModelCap struct {
	MaxContextTokens int
	SupportsFunctions bool
	IsLong           bool // e.g. 128k+ context
}

// HarnessComposer composes a GoalExecProfile from a TaskAnalysis.
// It is deterministic: same inputs → same profile (no random).
type HarnessComposer struct{}

func NewComposer() *HarnessComposer { return &HarnessComposer{} }

// Compose analyses the task and returns a recommended GoalExecProfile.
func (hc *HarnessComposer) Compose(a TaskAnalysis) GoalExecProfile {
	p := GoalExecProfile{Mode: ProfileAuto}
	var reasons []string

	// ── Context ──────────────────────────────────────────────────────────────

	// Always start with selective context.
	p.Context |= SelectiveContext
	reasons = append(reasons, "selective context (default)")

	// Large or complex repos → repo map.
	if a.RepositoryFiles > 100 || a.DependencyDepth > 3 {
		p.Context |= RepositoryMap
		reasons = append(reasons, "repo map (large repo or deep deps)")
	}

	// Long-running tasks need fresh context.
	if a.EstimatedMinutes > 30 || a.Complexity > 0.7 {
		p.Context |= FreshContext
		reasons = append(reasons, "fresh context (long/complex task)")
	}

	// High memory tasks.
	if a.Complexity > 0.8 {
		p.Context |= MemoryHeavy
		reasons = append(reasons, "memory heavy (very complex)")
	}

	// Large context window if model supports it.
	if a.PrimaryModelCap.IsLong || a.PrimaryModelCap.MaxContextTokens >= 128_000 {
		p.Context |= LargeContext
		reasons = append(reasons, "large context (model supports 128k+)")
	}

	// ── Execution ────────────────────────────────────────────────────────────

	switch {
	case a.Complexity < 0.25 && a.RiskLevel < 0.3:
		// Small, safe task → Direct.
		p.Execution |= Direct
		reasons = append(reasons, "direct execution (low complexity+risk)")

	case a.Complexity >= 0.25 && a.Complexity < 0.6:
		// Medium task → PlanExecute.
		p.Execution |= PlanExecute
		reasons = append(reasons, "plan+execute (medium complexity)")

	default:
		// Hard or risky → GoalLoop + PlanExecute.
		p.Execution |= GoalLoop | PlanExecute
		reasons = append(reasons, "goal loop+plan (high complexity/risk)")
	}

	// Parallel agents if task is large and autonomy is high.
	if a.AffectedFiles > 10 && a.RequestedAutonomy > 0.5 {
		p.Execution |= Parallel
		reasons = append(reasons, "parallel agents (many files, high autonomy)")
	}

	// Sandboxed if risk is high.
	if a.RiskLevel > 0.5 || a.TaskType == "refactor" {
		p.Execution |= Sandboxed
		reasons = append(reasons, "sandboxed worktree (risk/refactor)")
	}

	// Delegated for research tasks.
	if a.TaskType == "research" {
		p.Execution |= Delegated
		reasons = append(reasons, "delegated (research task)")
	}

	// ── Tools ────────────────────────────────────────────────────────────────

	switch a.TaskType {
	case "research":
		p.Tools |= ResearchTools
		reasons = append(reasons, "research tools")
	default:
		p.Tools |= CodingTools
		reasons = append(reasons, "coding tools")
	}

	if a.AffectedFiles > 5 && p.HasExec(Parallel) {
		p.Tools |= ParallelTools
		reasons = append(reasons, "parallel tool calls (parallel execution)")
	} else {
		p.Tools |= SequentialTools
		reasons = append(reasons, "sequential tool calls")
	}

	if a.RiskLevel > 0.6 {
		p.Tools |= RestrictedTools
		reasons = append(reasons, "restricted tools (high risk)")
	}

	// ── Verification ─────────────────────────────────────────────────────────

	switch {
	case a.Complexity < 0.2 && a.RiskLevel < 0.2 && a.TaskType != "bug_fix" && a.TaskType != "test":
		p.Verify |= FastVerify
		reasons = append(reasons, "fast verify (trivial non-code task)")
	case a.RiskLevel < 0.5:
		p.Verify |= TestVerify
		reasons = append(reasons, "test verify")
	default:
		p.Verify |= TestVerify | IndependentReview
		reasons = append(reasons, "test verify + independent review (high risk)")
	}

	// High-stakes tasks need artifacts.
	if a.RiskLevel > 0.7 || a.RequestedAutonomy > 0.8 {
		p.Verify |= ArtifactVerify
		reasons = append(reasons, "artifact verify (high risk/autonomy)")
	}

	// ── Recovery ─────────────────────────────────────────────────────────────

	// Always include Retry.
	p.Recovery |= Retry
	reasons = append(reasons, "retry (always)")

	if a.EstimatedMinutes > 15 {
		p.Recovery |= CheckpointResume
		reasons = append(reasons, "checkpoint resume (long task)")
	}
	if a.Complexity > 0.5 {
		p.Recovery |= Replan
		reasons = append(reasons, "replan (complex task)")
	}
	if a.Complexity > 0.7 {
		p.Recovery |= ContextReset
		reasons = append(reasons, "context reset (very complex)")
	}
	if a.FallbackModelRef != "" {
		p.Recovery |= ModelFallback
		p.FallbackModelRefs = []string{a.FallbackModelRef}
		reasons = append(reasons, "model fallback (fallback model configured)")
	}
	// High complexity warrants model fallback even without explicit config.
	if a.Complexity > 0.8 && !p.HasRecovery(ModelFallback) {
		p.Recovery |= ModelFallback
		reasons = append(reasons, "model fallback (high complexity)")
	}
	if a.RiskLevel > 0.8 {
		p.Recovery |= Escalation
		reasons = append(reasons, "escalation (very high risk)")
	}

	// ── Incorporate historical signals ────────────────────────────────────────

	if a.HistoricalSuccessRate > 0 && a.HistoricalSuccessRate < 0.5 {
		// This class of task has a poor track record — add more safety.
		if !p.HasVerify(IndependentReview) {
			p.Verify |= IndependentReview
			reasons = append(reasons, "independent review (poor historical success rate)")
		}
		if !p.HasRecovery(Replan) {
			p.Recovery |= Replan
			reasons = append(reasons, "replan (historical failures)")
		}
	}

	for _, sig := range a.PreviousStuckSignals {
		sd := NewStuckDetector()
		mutated, reason := sd.SuggestMutation(p, []StuckSignal{sig})
		if reason != "" {
			p = mutated
			reasons = append(reasons, "historical stuck "+string(sig)+": "+reason)
		}
	}

	// ── Context budget ────────────────────────────────────────────────────────

	if a.TokenBudget > 0 {
		p.ContextBudgetTokens = a.TokenBudget
	} else {
		p.ContextBudgetTokens = p.EffectiveContextBudget()
	}

	// ── Max parallel agents ───────────────────────────────────────────────────

	if p.HasExec(Parallel) {
		workers := clampInt(int(math.Ceil(float64(a.AffectedFiles)/5.0)), 2, 8)
		p.MaxParallelAgents = workers
		reasons = append(reasons, "parallel agents: "+itoa(workers))
	}

	p.ComposerReason = strings.Join(reasons, "; ")
	return p
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}