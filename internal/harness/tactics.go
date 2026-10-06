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
	// SelectiveContext: list only the files that match the task's keywords.
	SelectiveContext ContextTactic = 1 << iota
	// LargeContext: a larger budget for the files, map and chat history an
	// iteration carries.
	LargeContext
	// FreshContext: each iteration sees only its own turn of the chat plus a
	// short hand-off of earlier progress, instead of the growing history.
	FreshContext
	// RepositoryMap: include a structural map of the repository.
	RepositoryMap
	// MemoryHeavy: carry every earlier iteration's progress line forward
	// (not just the last one). Saved memory notes are always included.
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
	// Parallel and Delegated are retired: parallel agents are Teamwork's
	// job. They stay so stored profiles still decode; nothing sets or reads
	// them.
	Parallel
	Delegated
	// Sandboxed: snapshot the workspace before the goal starts (undo with
	// /restore) and keep the agent inside it.
	Sandboxed
)

// ── Tool tactics ─────────────────────────────────────────────────────────────

// ToolTactic controls how tools are invoked.
type ToolTactic uint32

const (
	// SequentialTools: one tool call at a time.
	SequentialTools ToolTactic = 1 << iota
	// ParallelTools: a turn's read-only tool calls run concurrently.
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
	// FastVerify: quality gates run only once the reviewer finds the
	// criteria met, not every iteration.
	FastVerify VerifyTactic = 1 << iota
	// TestVerify: run the contract's test/build quality gates every iteration.
	TestVerify
	// IndependentReview: the reviewer (a fresh, tool-less model call) reads
	// the diff itself, not only the agent's summary and the diff stat.
	IndependentReview
	// DoubleReview: two independent reviewers must agree.
	DoubleReview
	// ArtifactVerify: done needs evidence in the workspace — changed files.
	ArtifactVerify
)

// ── Recovery tactics ─────────────────────────────────────────────────────────

// RecoveryTactic controls what happens on failure.
type RecoveryTactic uint32

const (
	// Retry: run a failed agent run again (bounded by the plan's retries).
	Retry RecoveryTactic = 1 << iota
	// Replan: after a failure or when stuck, the next iteration is told to
	// drop its approach and plan again.
	Replan
	// ModelFallback: after a failure or when stuck, switch to another
	// configured model.
	ModelFallback
	// ContextReset: after a failure, the next iteration starts from a fresh
	// context plus the hand-off.
	ContextReset
	// CheckpointResume: a restarted or resumed goal picks up from its last
	// persisted checkpoint (iteration and progress).
	CheckpointResume
	// Escalation: when still stuck after the harness has adapted, block and
	// ask the user.
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

// GoalExecProfile is the central execution policy for a Goal.
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
