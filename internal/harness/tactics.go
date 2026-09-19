// Package harness implements Rove Code's central execution policy system.
// A HarnessProfile is a composition of tactic flags — not a single enum.
// Multiple tactics within the same category can be active simultaneously.
// The ExecutionKernel reads the profile and dispatches real behavior;
// the StuckDetector watches progress and mutates the profile when needed.
package harness

// ── Context tactics ──────────────────────────────────────────────────────────

// ContextTactic controls what context the agent receives each iteration.
type ContextTactic uint32

const (
	// SelectiveContext: filter workspace to only relevant files/rules.
	SelectiveContext ContextTactic = 1 << iota
	// LargeContext: allocate a higher token budget for context window.
	LargeContext
	// FreshContext: start a new model context via checkpoint+handoff.
	FreshContext
	// RepositoryMap: include a structural map of the repository.
	RepositoryMap
	// MemoryHeavy: load all available agent memory into context.
	MemoryHeavy
)

// ── Execution tactics ────────────────────────────────────────────────────────

// ExecutionTactic controls how the agent executes work.
type ExecutionTactic uint32

const (
	// Direct: execute without building a structured plan.
	Direct ExecutionTactic = 1 << iota
	// PlanExecute: generate a structured plan then execute it step by step.
	PlanExecute
	// GoalLoop: loop until judge says DONE, ignoring agent completion claims.
	GoalLoop
	// Parallel: run independent subtasks as parallel sub-agents.
	Parallel
	// Delegated: hand subtasks to separate specialized agents.
	Delegated
	// Sandboxed: run in an isolated worktree/workspace.
	Sandboxed
)

// ── Tool tactics ─────────────────────────────────────────────────────────────

// ToolTactic controls how tools are invoked.
type ToolTactic uint32

const (
	// SequentialTools: one tool call at a time.
	SequentialTools ToolTactic = 1 << iota
	// ParallelTools: safe independent tool calls run concurrently.
	ParallelTools
	// RestrictedTools: only the explicitly allowed tool set is exposed.
	RestrictedTools
	// CodingTools: expose file/shell/search/git tools; restrict browser/network.
	CodingTools
	// ResearchTools: expose browser/web/search tools.
	ResearchTools
)

// ── Verification tactics ─────────────────────────────────────────────────────

// VerifyTactic controls how results are verified.
type VerifyTactic uint32

const (
	// FastVerify: minimal verification (syntax check only).
	FastVerify VerifyTactic = 1 << iota
	// TestVerify: run task-specific test/build quality gates.
	TestVerify
	// IndependentReview: spawn a separate fresh-context reviewer agent.
	IndependentReview
	// DoubleReview: two independent reviewers must agree.
	DoubleReview
	// ArtifactVerify: require diff/log/screenshot/build artifact evidence.
	ArtifactVerify
)

// ── Recovery tactics ─────────────────────────────────────────────────────────

// RecoveryTactic controls what happens on failure.
type RecoveryTactic uint32

const (
	// Retry: retry the last action on transient failure.
	Retry RecoveryTactic = 1 << iota
	// Replan: rebuild the execution plan after a failure.
	Replan
	// ModelFallback: switch to a fallback model from a checkpoint.
	ModelFallback
	// ContextReset: start a fresh model context + handoff state.
	ContextReset
	// CheckpointResume: resume from the last persisted execution checkpoint.
	CheckpointResume
	// Escalation: block and wait for human intervention.
	Escalation
)

// ── Profile mode ─────────────────────────────────────────────────────────────

// ProfileMode distinguishes user-authored from auto-composed profiles.
type ProfileMode string

const (
	ProfileManual ProfileMode = "manual"
	ProfileAuto   ProfileMode = "auto"
)

// ── Composed profile ─────────────────────────────────────────────────────────

// GoalExecProfile is the central execution policy for a Goal or Kanban Card.
// Each field is a bitmask so multiple tactics can be active simultaneously.
// E.g. Context = SelectiveContext | RepositoryMap is valid.
type GoalExecProfile struct {
	Mode ProfileMode `json:"mode"`

	// Bitmask fields — combine freely.
	Context   ContextTactic   `json:"context"`
	Execution ExecutionTactic `json:"execution"`
	Tools     ToolTactic      `json:"tools"`
	Verify    VerifyTactic    `json:"verify"`
	Recovery  RecoveryTactic  `json:"recovery"`

	// Runtime limits derived from tactics.
	ContextBudgetTokens int `json:"contextBudgetTokens,omitempty"`
	MaxParallelAgents   int `json:"maxParallelAgents,omitempty"`
	MaxToolConcurrency  int `json:"maxToolConcurrency,omitempty"`

	// Auto-composed metadata (set by HarnessComposer).
	ComposerReason    string   `json:"composerReason,omitempty"`
	AllowedTools      []string `json:"allowedTools,omitempty"`
	FallbackModelRefs []string `json:"fallbackModelRefs,omitempty"`

	// Version counter — incremented on every mutation.
	Version int `json:"version"`
}

// Has reports whether a ContextTactic flag is set.
func (p GoalExecProfile) Has(t ContextTactic) bool { return p.Context&t != 0 }

// HasExec reports whether an ExecutionTactic flag is set.
func (p GoalExecProfile) HasExec(t ExecutionTactic) bool { return p.Execution&t != 0 }

// HasTool reports whether a ToolTactic flag is set.
func (p GoalExecProfile) HasTool(t ToolTactic) bool { return p.Tools&t != 0 }

// HasVerify reports whether a VerifyTactic flag is set.
func (p GoalExecProfile) HasVerify(t VerifyTactic) bool { return p.Verify&t != 0 }

// HasRecovery reports whether a RecoveryTactic flag is set.
func (p GoalExecProfile) HasRecovery(t RecoveryTactic) bool { return p.Recovery&t != 0 }

// ── Mutation helpers ──────────────────────────────────────────────────────────
// Pointer receivers so callers can build profiles inline.

func (p *GoalExecProfile) AddContext(t ContextTactic)     { p.Context |= t }
func (p *GoalExecProfile) AddExecution(t ExecutionTactic) { p.Execution |= t }
func (p *GoalExecProfile) AddTools(t ToolTactic)          { p.Tools |= t }
func (p *GoalExecProfile) AddVerify(t VerifyTactic)       { p.Verify |= t }
func (p *GoalExecProfile) AddRecovery(t RecoveryTactic)   { p.Recovery |= t }

// Explain returns a one-line human-readable description of the active tactics.
func (p GoalExecProfile) Explain() string {
	return p.contextExplain() + " | " + p.execExplain() + " | " + p.toolsExplain() + " | " + p.verifyExplain() + " | " + p.recoveryExplain()
}

func (p GoalExecProfile) contextExplain() string {
	var parts []string
	if p.Has(SelectiveContext) {
		parts = append(parts, "Selective")
	}
	if p.Has(LargeContext) {
		parts = append(parts, "Large")
	}
	if p.Has(FreshContext) {
		parts = append(parts, "Fresh")
	}
	if p.Has(RepositoryMap) {
		parts = append(parts, "RepoMap")
	}
	if p.Has(MemoryHeavy) {
		parts = append(parts, "Memory")
	}
	if len(parts) == 0 {
		return "Ctx: default"
	}
	return "Ctx: " + joinParts(parts)
}

func (p GoalExecProfile) execExplain() string {
	var parts []string
	if p.HasExec(Direct) {
		parts = append(parts, "Direct")
	}
	if p.HasExec(PlanExecute) {
		parts = append(parts, "Plan")
	}
	if p.HasExec(GoalLoop) {
		parts = append(parts, "Loop")
	}
	if p.HasExec(Parallel) {
		parts = append(parts, "Parallel")
	}
	if p.HasExec(Delegated) {
		parts = append(parts, "Delegated")
	}
	if p.HasExec(Sandboxed) {
		parts = append(parts, "Sandboxed")
	}
	if len(parts) == 0 {
		return "Exec: default"
	}
	return "Exec: " + joinParts(parts)
}

func (p GoalExecProfile) toolsExplain() string {
	var parts []string
	if p.HasTool(SequentialTools) {
		parts = append(parts, "Seq")
	}
	if p.HasTool(ParallelTools) {
		parts = append(parts, "Parallel")
	}
	if p.HasTool(RestrictedTools) {
		parts = append(parts, "Restricted")
	}
	if p.HasTool(CodingTools) {
		parts = append(parts, "Coding")
	}
	if p.HasTool(ResearchTools) {
		parts = append(parts, "Research")
	}
	if len(parts) == 0 {
		return "Tools: default"
	}
	return "Tools: " + joinParts(parts)
}

func (p GoalExecProfile) verifyExplain() string {
	var parts []string
	if p.HasVerify(FastVerify) {
		parts = append(parts, "Fast")
	}
	if p.HasVerify(TestVerify) {
		parts = append(parts, "Test")
	}
	if p.HasVerify(IndependentReview) {
		parts = append(parts, "IndepReview")
	}
	if p.HasVerify(DoubleReview) {
		parts = append(parts, "Double")
	}
	if p.HasVerify(ArtifactVerify) {
		parts = append(parts, "Artifact")
	}
	if len(parts) == 0 {
		return "Verify: default"
	}
	return "Verify: " + joinParts(parts)
}

func (p GoalExecProfile) recoveryExplain() string {
	var parts []string
	if p.HasRecovery(Retry) {
		parts = append(parts, "Retry")
	}
	if p.HasRecovery(Replan) {
		parts = append(parts, "Replan")
	}
	if p.HasRecovery(ModelFallback) {
		parts = append(parts, "ModelFB")
	}
	if p.HasRecovery(ContextReset) {
		parts = append(parts, "CtxReset")
	}
	if p.HasRecovery(CheckpointResume) {
		parts = append(parts, "Checkpoint")
	}
	if p.HasRecovery(Escalation) {
		parts = append(parts, "Escalate")
	}
	if len(parts) == 0 {
		return "Recovery: none"
	}
	return "Recovery: " + joinParts(parts)
}

func joinParts(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "+"
		}
		result += p
	}
	return result
}

// EffectiveContextBudget returns the resolved token budget for context.
func (p GoalExecProfile) EffectiveContextBudget() int {
	if p.ContextBudgetTokens > 0 {
		return p.ContextBudgetTokens
	}
	if p.Has(LargeContext) {
		return 200_000
	}
	if p.Has(SelectiveContext) {
		return 32_000
	}
	return 64_000
}

// EffectiveToolConcurrency returns the max concurrent tool calls.
func (p GoalExecProfile) EffectiveToolConcurrency() int {
	if p.MaxToolConcurrency > 0 {
		return p.MaxToolConcurrency
	}
	if p.HasTool(ParallelTools) {
		return 8
	}
	return 1
}

// EffectiveMaxParallelAgents returns max parallel sub-agents.
func (p GoalExecProfile) EffectiveMaxParallelAgents() int {
	if p.MaxParallelAgents > 0 {
		return p.MaxParallelAgents
	}
	if p.HasExec(Parallel) {
		return 4
	}
	return 1
}

// ── Predefined profiles ───────────────────────────────────────────────────────

// SmallBugProfile is for small, well-scoped bug fixes.
func SmallBugProfile() GoalExecProfile {
	return GoalExecProfile{
		Mode:           ProfileAuto,
		Context:        SelectiveContext,
		Execution:      Direct,
		Tools:          CodingTools | SequentialTools,
		Verify:         TestVerify,
		Recovery:       Retry,
		ComposerReason: "small bug: selective context, direct execution, test verify, retry on failure",
	}
}

// LargeRefactorProfile is for large, cross-cutting refactors.
func LargeRefactorProfile() GoalExecProfile {
	return GoalExecProfile{
		Mode:           ProfileAuto,
		Context:        RepositoryMap | SelectiveContext,
		Execution:      PlanExecute | Parallel | Sandboxed,
		Tools:          CodingTools | ParallelTools,
		Verify:         TestVerify | IndependentReview,
		Recovery:       CheckpointResume | Replan,
		ComposerReason: "large refactor: repo map, plan+execute, parallel worktrees, independent review, checkpoint resume",
	}
}

// HardLongTaskProfile is for complex, multi-day autonomous tasks.
func HardLongTaskProfile() GoalExecProfile {
	return GoalExecProfile{
		Mode:           ProfileAuto,
		Context:        SelectiveContext | RepositoryMap | FreshContext,
		Execution:      GoalLoop | Delegated | Parallel,
		Tools:          CodingTools | ParallelTools,
		Verify:         IndependentReview | ArtifactVerify,
		Recovery:       Replan | ContextReset | ModelFallback | CheckpointResume,
		ComposerReason: "hard/long task: multi-context loop, delegated parallel agents, full verification, full recovery stack",
	}
}
