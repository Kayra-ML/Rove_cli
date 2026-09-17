package harness

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ExecutionState tracks per-iteration observations for the stuck detector.
type ExecutionState struct {
	GoalID       string
	Iteration    int
	Errors       []string
	FailedGates  []string
	FilesModified [][]string // per iteration
	ToolCalls    [][]string // per iteration
	Summaries    []string   // per iteration
	mu           sync.Mutex
}

func NewExecutionState(goalID string) *ExecutionState {
	return &ExecutionState{GoalID: goalID}
}

// RecordIteration appends one iteration's observations.
func (es *ExecutionState) RecordIteration(errs, gates []string, files, tools []string, summary string) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.Errors = append(es.Errors, errs...)
	es.FailedGates = append(es.FailedGates, gates...)
	es.FilesModified = append(es.FilesModified, files)
	es.ToolCalls = append(es.ToolCalls, tools)
	es.Summaries = append(es.Summaries, summary)
	es.Iteration++
}

// Window returns the observation window for the stuck detector.
func (es *ExecutionState) Window() ObservationWindow {
	es.mu.Lock()
	defer es.mu.Unlock()
	return ObservationWindow{
		Errors:        append([]string{}, es.Errors...),
		FailedGates:   append([]string{}, es.FailedGates...),
		FilesModified: append([][]string{}, es.FilesModified...),
		ToolCalls:     append([][]string{}, es.ToolCalls...),
		Summaries:     append([]string{}, es.Summaries...),
	}
}

// ── Execution plan (PlanExecute tactic) ───────────────────────────────────────

// ExecutionStep is one step in a structured plan.
type ExecutionStep struct {
	Index       int      `json:"index"`
	Description string   `json:"description"`
	Files       []string `json:"files,omitempty"`
	Done        bool     `json:"done"`
}

// ExecutionPlan is the structured plan for PlanExecute mode.
type ExecutionPlan struct {
	GoalTitle string          `json:"goalTitle"`
	Steps     []ExecutionStep `json:"steps"`
	CreatedAt time.Time       `json:"createdAt"`
}

// buildPlan creates a simple execution plan from goal description.
// In a full implementation this would call the LLM to decompose the goal.
// Here we produce a deterministic plan from acceptance criteria.
func buildPlan(goalTitle, goalDesc string, criteria []string) ExecutionPlan {
	steps := make([]ExecutionStep, 0, len(criteria)+2)
	steps = append(steps, ExecutionStep{Index: 0, Description: "Analyse task: " + goalTitle})
	for i, c := range criteria {
		steps = append(steps, ExecutionStep{Index: i + 1, Description: "Implement: " + c})
	}
	steps = append(steps, ExecutionStep{Index: len(steps), Description: "Verify all criteria and run quality gates"})
	return ExecutionPlan{GoalTitle: goalTitle, Steps: steps, CreatedAt: time.Now().UTC()}
}

// ── IterationConfig: what the kernel passes to the agent each iteration ────────

// IterationConfig bundles all resolved policy decisions for one iteration.
type IterationConfig struct {
	// From context strategy.
	RunCtx RunContext
	// From tool policy.
	ToolCfg ToolExecConfig
	// From verify strategy.
	VerifyCfg VerifyConfig
	// From recovery strategy.
	RecoveryPlan RecoveryPlan
	// Execution plan (may be nil for Direct mode).
	Plan *ExecutionPlan
	// Current step index (PlanExecute mode).
	CurrentStep int
	// System extra appended to agent system prompt.
	SystemExtra string
	// Max turns per agent invocation.
	MaxTurns int
}

// ── ExecutionKernel ────────────────────────────────────────────────────────────

// AgentRunner is the interface the kernel uses to run an agent.
// Matches the agent.Runtime.Run signature shape.
type AgentRunner interface {
	RunAgent(ctx context.Context, req AgentRunRequest) (AgentRunResult, error)
}

// AgentRunRequest is what the kernel sends to the agent.
type AgentRunRequest struct {
	AgentID      string
	SessionID    string
	WorkspaceID  string
	WorkDir      string
	CardID       string
	UserMessage  string
	SystemExtra  string
	MaxTurns     int
	AllowedTools []string
	ContextBudget int
}

// AgentRunResult is the agent's output.
type AgentRunResult struct {
	Summary     string
	ToolsCalled []string
	FilesEdited []string
	Errors      []string
	Done        bool
}

// JudgeRunner is the interface the kernel uses to judge results.
type JudgeRunner interface {
	Judge(ctx context.Context, goalID string, iteration int,
		evidence JudgeEvidence, workDir string) JudgeResult
}

// JudgeEvidence feeds the judge.
type JudgeEvidence struct {
	AgentClaimedDone bool
	Summary          string
	FailedGates      []string
	Artifacts        []string
}

// JudgeResult from the judge runner.
type JudgeResult struct {
	Decision  string // "DONE" | "CONTINUE" | "BLOCKED"
	Reason    string
	GateFails []string
}

// KernelOpts configure the ExecutionKernel.
type KernelOpts struct {
	GoalID      string
	CardID      string
	AgentID     string
	SessionID   string
	WorkspaceID string
	WorkDir     string
	Title       string
	Description string
	Criteria    []string
	MaxIterations int

	Profile    GoalExecProfile
	AllTools   []string
	RepoFiles  []string
	Keywords   []string
	Checkpoints *CheckpointStore
}

// ExecutionKernel is the policy-driven execution engine.
// It reads the HarnessProfile and dispatches real agent runs accordingly.
type ExecutionKernel struct {
	opts    KernelOpts
	harness *HarnessProfile
	state   *ExecutionState
	stuck   *StuckDetector
}

// NewKernel creates a new ExecutionKernel.
func NewKernel(opts KernelOpts) *ExecutionKernel {
	hp := &HarnessProfile{
		GoalID:    opts.GoalID,
		CardID:    opts.CardID,
		Current:   opts.Profile,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	return &ExecutionKernel{
		opts:    opts,
		harness: hp,
		state:   NewExecutionState(opts.GoalID),
		stuck:   NewStuckDetector(),
	}
}

// Profile returns the current (possibly mutated) harness profile.
func (k *ExecutionKernel) Profile() *HarnessProfile { return k.harness }

// ResolveIteration computes the IterationConfig for the current profile.
func (k *ExecutionKernel) ResolveIteration(iteration int) IterationConfig {
	p := k.harness.Current

	// Context.
	ce := &ContextEngine{
		Profile:      p,
		WorkspaceDir: k.opts.WorkDir,
		RepoFiles:    k.opts.RepoFiles,
	}
	runCtx := ce.Build(context.Background(), k.opts.Keywords)

	// Tools.
	tp := &ToolPolicy{Profile: p, AllAvailableTools: k.opts.AllTools}
	toolCfg := tp.Resolve()

	// Verify.
	vs := &VerifyStrategy{Profile: p}
	verifyCfg := vs.Resolve()

	// Recovery.
	rs := &RecoveryStrategy{Profile: p}
	recoveryPlan := rs.Resolve()

	// Execution plan.
	var plan *ExecutionPlan
	currentStep := 0
	if p.HasExec(PlanExecute) && iteration == 1 {
		pl := buildPlan(k.opts.Title, k.opts.Description, k.opts.Criteria)
		plan = &pl
	}

	// System prompt extra.
	systemExtra := k.buildSystemExtra(p, verifyCfg, iteration)

	maxTurns := 12
	if p.HasExec(GoalLoop) {
		maxTurns = 24
	}

	return IterationConfig{
		RunCtx:       runCtx,
		ToolCfg:      toolCfg,
		VerifyCfg:    verifyCfg,
		RecoveryPlan: recoveryPlan,
		Plan:         plan,
		CurrentStep:  currentStep,
		SystemExtra:  systemExtra,
		MaxTurns:     maxTurns,
	}
}

// ObserveIteration records one iteration's observations and runs the stuck detector.
// Returns (mutated bool, mutation reason) if the profile was mutated.
func (k *ExecutionKernel) ObserveIteration(
	iteration int,
	errs, failedGates, filesEdited, toolsCalled []string,
	summary string,
) (bool, string) {
	k.state.RecordIteration(errs, failedGates, filesEdited, toolsCalled, summary)
	signals := k.stuck.Detect(k.state.Window())
	if len(signals) == 0 {
		return false, ""
	}
	newProfile, reason := k.stuck.SuggestMutation(k.harness.Current, signals)
	if reason == "" {
		return false, ""
	}
	k.harness.Mutate(newProfile, reason, iteration)
	return true, reason
}

// Mutations returns the full mutation trace.
func (k *ExecutionKernel) Mutations() []HarnessMutation {
	return k.harness.Mutations
}

// Explain returns a human-readable summary of the current profile.
func (k *ExecutionKernel) Explain() string {
	p := k.harness.Current
	var parts []string
	parts = append(parts, fmt.Sprintf("Mode: %s (v%d)", p.Mode, p.Version))
	parts = append(parts, contextExplain(p))
	parts = append(parts, execExplain(p))
	parts = append(parts, toolExplain(p))
	parts = append(parts, verifyExplain(p))
	parts = append(parts, recoveryExplain(p))
	if p.ComposerReason != "" {
		parts = append(parts, "Composer: "+p.ComposerReason)
	}
	return strings.Join(parts, "\n")
}

func (k *ExecutionKernel) buildSystemExtra(p GoalExecProfile, vc VerifyConfig, iteration int) string {
	var sb strings.Builder
	sb.WriteString("Execution policy for this run:\n")
	sb.WriteString("  Context: " + contextExplain(p) + "\n")
	sb.WriteString("  Execution: " + execExplain(p) + "\n")
	sb.WriteString("  Tools: " + toolExplain(p) + "\n")
	sb.WriteString("  Verify: " + vc.Explanation + "\n")
	sb.WriteString(fmt.Sprintf("  Iteration: %d", iteration))
	if k.opts.MaxIterations > 0 {
		sb.WriteString(fmt.Sprintf(" of %d", k.opts.MaxIterations))
	}
	sb.WriteString("\nDo not declare the goal complete. Quality gates and the judge decide.")
	return sb.String()
}

// ── tactic explain helpers ─────────────────────────────────────────────────────

func contextExplain(p GoalExecProfile) string {
	var parts []string
	if p.Has(SelectiveContext) { parts = append(parts, "Selective") }
	if p.Has(LargeContext)     { parts = append(parts, "Large") }
	if p.Has(FreshContext)     { parts = append(parts, "Fresh") }
	if p.Has(RepositoryMap)   { parts = append(parts, "RepoMap") }
	if p.Has(MemoryHeavy)     { parts = append(parts, "MemoryHeavy") }
	if len(parts) == 0 { return "Context: default" }
	return "Context: " + strings.Join(parts, "+")
}

func execExplain(p GoalExecProfile) string {
	var parts []string
	if p.HasExec(Direct)       { parts = append(parts, "Direct") }
	if p.HasExec(PlanExecute)  { parts = append(parts, "PlanExecute") }
	if p.HasExec(GoalLoop)     { parts = append(parts, "GoalLoop") }
	if p.HasExec(Parallel)     { parts = append(parts, "Parallel") }
	if p.HasExec(Delegated)    { parts = append(parts, "Delegated") }
	if p.HasExec(Sandboxed)    { parts = append(parts, "Sandboxed") }
	if len(parts) == 0 { return "Exec: default" }
	return "Exec: " + strings.Join(parts, "+")
}

func toolExplain(p GoalExecProfile) string {
	var parts []string
	if p.HasTool(SequentialTools) { parts = append(parts, "Sequential") }
	if p.HasTool(ParallelTools)   { parts = append(parts, "Parallel") }
	if p.HasTool(RestrictedTools) { parts = append(parts, "Restricted") }
	if p.HasTool(CodingTools)     { parts = append(parts, "Coding") }
	if p.HasTool(ResearchTools)   { parts = append(parts, "Research") }
	if len(parts) == 0 { return "Tools: all" }
	return "Tools: " + strings.Join(parts, "+")
}

func verifyExplain(p GoalExecProfile) string {
	var parts []string
	if p.HasVerify(FastVerify)        { parts = append(parts, "Fast") }
	if p.HasVerify(TestVerify)        { parts = append(parts, "Test") }
	if p.HasVerify(IndependentReview) { parts = append(parts, "IndependentReview") }
	if p.HasVerify(DoubleReview)      { parts = append(parts, "DoubleReview") }
	if p.HasVerify(ArtifactVerify)    { parts = append(parts, "Artifact") }
	if len(parts) == 0 { return "Verify: default" }
	return "Verify: " + strings.Join(parts, "+")
}

func recoveryExplain(p GoalExecProfile) string {
	var parts []string
	if p.HasRecovery(Retry)            { parts = append(parts, "Retry") }
	if p.HasRecovery(Replan)           { parts = append(parts, "Replan") }
	if p.HasRecovery(ModelFallback)    { parts = append(parts, "ModelFallback") }
	if p.HasRecovery(ContextReset)     { parts = append(parts, "ContextReset") }
	if p.HasRecovery(CheckpointResume) { parts = append(parts, "Checkpoint") }
	if p.HasRecovery(Escalation)       { parts = append(parts, "Escalate") }
	if len(parts) == 0 { return "Recovery: none" }
	return "Recovery: " + strings.Join(parts, "+")
}