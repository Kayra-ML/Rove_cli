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
	GoalID        string
	Iteration     int
	Errors        []string
	FailedGates   []string
	FilesModified [][]string // per iteration
	ToolCalls     [][]string // per iteration
	Summaries     []string   // per iteration
	mu            sync.Mutex
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

// buildPlan lays out the goal's criteria as steps: analyse, one step per
// criterion, verify. The agent is asked to refine it in its own words; this
// keeps the iterations anchored to the contract without an extra model call.
func buildPlan(goalTitle string, criteria []string) ExecutionPlan {
	steps := make([]ExecutionStep, 0, len(criteria)+2)
	steps = append(steps, ExecutionStep{Index: 0, Description: "Analyse the task: " + goalTitle})
	for i, c := range criteria {
		steps = append(steps, ExecutionStep{Index: i + 1, Description: "Implement: " + c})
	}
	steps = append(steps, ExecutionStep{Index: len(steps), Description: "Verify every criterion and run the checks"})
	return ExecutionPlan{GoalTitle: goalTitle, Steps: steps, CreatedAt: time.Now().UTC()}
}

// ── IterationConfig: what the kernel passes to the agent each iteration ────────

// IterationConfig bundles all resolved policy decisions for one iteration.
type IterationConfig struct {
	RunCtx       RunContext
	ToolCfg      ToolExecConfig
	VerifyCfg    VerifyConfig
	RecoveryPlan RecoveryPlan
	// Plan is the PlanExecute plan (nil otherwise); CurrentStep is the step
	// this iteration works on. Replanned is set on the iteration after the
	// harness asked for a new plan.
	Plan        *ExecutionPlan
	CurrentStep int
	Replanned   bool
	// HistoryTurns is how many user turns of the chat the run sees (see
	// agent.RunRequest.HistoryTurns). Fresh is set when that is only this
	// iteration's own, so the prompt must carry the hand-off.
	HistoryTurns int
	Fresh        bool
	// UseFallbackModel asks the engine to run this iteration on another
	// configured model.
	UseFallbackModel bool
	SystemExtra      string
	MaxTurns         int
}

// KernelOpts configure the ExecutionKernel.
type KernelOpts struct {
	GoalID        string
	WorkDir       string
	Title         string
	Criteria      []string
	MaxIterations int

	Profile GoalExecProfile
	// Harness, when set, is the goal's persisted harness (its profile and
	// the mutations so far); the kernel continues it instead of starting a
	// new trace.
	Harness   *HarnessProfile
	AllTools  []string
	RepoFiles []string
	Keywords  []string
}

// ExecutionKernel is the policy-driven execution engine.
// It reads the HarnessProfile and dispatches real agent runs accordingly.
type ExecutionKernel struct {
	opts    KernelOpts
	harness *HarnessProfile
	state   *ExecutionState
	stuck   *StuckDetector

	plan      *ExecutionPlan
	planStart int
	// set by a failure or a stuck signal, used by the next iteration
	replan, fresh, fallback bool
	escalate                bool
}

// NewKernel creates a new ExecutionKernel.
func NewKernel(opts KernelOpts) *ExecutionKernel {
	hp := opts.Harness
	if hp == nil {
		hp = &HarnessProfile{
			GoalID:    opts.GoalID,
			Current:   opts.Profile,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
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

// History budgets, in user turns of the chat an iteration sees.
const (
	turnsDefault = 3
	turnsLarge   = 6
)

// ResolveIteration computes the IterationConfig for the current profile.
func (k *ExecutionKernel) ResolveIteration(iteration int) IterationConfig {
	p := k.harness.Current

	ce := &ContextEngine{Profile: p, WorkspaceDir: k.opts.WorkDir, RepoFiles: k.opts.RepoFiles}
	runCtx := ce.Build(context.Background(), k.opts.Keywords)
	toolCfg := (&ToolPolicy{Profile: p, AllAvailableTools: k.opts.AllTools}).Resolve()
	verifyCfg := (&VerifyStrategy{Profile: p}).Resolve()
	recoveryPlan := (&RecoveryStrategy{Profile: p}).Resolve()

	// the plan is made once and kept; a replan starts it over
	replanned := false
	if p.HasExec(PlanExecute) && (k.plan == nil || k.replan) {
		pl := buildPlan(k.opts.Title, k.opts.Criteria)
		replanned = k.plan != nil
		k.plan, k.planStart = &pl, iteration
	}
	step := 0
	if k.plan != nil {
		step = iteration - k.planStart
		if last := len(k.plan.Steps) - 1; step > last {
			step = last
		}
	}

	fresh := runCtx.FreshHandoff || k.fresh
	turns := turnsDefault
	if p.Has(LargeContext) {
		turns = turnsLarge
	}
	if fresh {
		turns = 1
	}

	cfg := IterationConfig{
		RunCtx:           runCtx,
		ToolCfg:          toolCfg,
		VerifyCfg:        verifyCfg,
		RecoveryPlan:     recoveryPlan,
		Plan:             k.plan,
		CurrentStep:      step,
		Replanned:        replanned || (k.replan && k.plan == nil),
		HistoryTurns:     turns,
		Fresh:            fresh,
		UseFallbackModel: k.fallback,
		MaxTurns:         MaxTurnsFromProfile(p),
	}
	cfg.SystemExtra = k.buildSystemExtra(p, verifyCfg, iteration)
	// one-shot adjustments are used up by this iteration
	k.replan, k.fresh, k.fallback = false, false, false
	return cfg
}

// OnFailure applies the recovery plan after an iteration whose agent run
// failed (after its retries): the next iteration may replan, start fresh or
// switch model. It reports whether the goal should stop instead (no
// recovery configured).
func (k *ExecutionKernel) OnFailure() (giveUp bool) {
	p := k.harness.Current
	if p.HasRecovery(Replan) {
		k.replan = true
	}
	if p.HasRecovery(ContextReset) {
		k.fresh = true
	}
	if p.HasRecovery(ModelFallback) {
		k.fallback = true
	}
	return !p.HasRecovery(Retry | Replan | ContextReset | ModelFallback | CheckpointResume)
}

// ObserveIteration records one iteration's observations and runs the stuck
// detector. When stuck it mutates the profile (reported as mutated+reason)
// and lines up the matching recovery for the next iteration. When stuck and
// there is nothing left to change, a profile with Escalation asks for the
// user (see Escalate).
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
		// stuck, and the harness has already tried what it knows
		if k.harness.Current.HasRecovery(Escalation) && len(k.harness.Mutations) > 0 {
			k.escalate = true
		}
		return false, ""
	}
	k.harness.Mutate(newProfile, reason, iteration)
	p := k.harness.Current
	for _, sig := range signals {
		switch sig {
		case StuckRepeatedError, StuckRepeatedTools, StuckPlanStale:
			k.replan = k.replan || p.HasRecovery(Replan)
		case StuckFileOscillation:
			k.fallback = k.fallback || p.HasRecovery(ModelFallback)
			k.fresh = true
		case StuckNoProgress:
			k.fresh = k.fresh || p.HasRecovery(ContextReset)
		}
	}
	return true, reason
}

// Escalate reports that the goal is stuck beyond what the harness can adapt
// to and the profile says to hand it to the user.
func (k *ExecutionKernel) Escalate() bool { return k.escalate }

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

// buildSystemExtra is the short policy note added to the system prompt. It
// is sent on every model turn, so it stays a few lines.
func (k *ExecutionKernel) buildSystemExtra(p GoalExecProfile, vc VerifyConfig, iteration int) string {
	var sb strings.Builder
	sb.WriteString("You are working on a goal in iterations.")
	sb.WriteString(fmt.Sprintf(" Iteration %d", iteration))
	if k.opts.MaxIterations > 0 {
		sb.WriteString(fmt.Sprintf(" of %d", k.opts.MaxIterations))
	}
	sb.WriteString(". An independent reviewer and the quality gates decide when it is done; your own claim does not.")
	if extra := SystemExtraFromProfile(p); extra != "" {
		sb.WriteString("\n" + extra)
	}
	return sb.String()
}

// ── tactic explain helpers ─────────────────────────────────────────────────────

func contextExplain(p GoalExecProfile) string {
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
		parts = append(parts, "MemoryHeavy")
	}
	if len(parts) == 0 {
		return "Context: default"
	}
	return "Context: " + strings.Join(parts, "+")
}

func execExplain(p GoalExecProfile) string {
	var parts []string
	if p.HasExec(Direct) {
		parts = append(parts, "Direct")
	}
	if p.HasExec(PlanExecute) {
		parts = append(parts, "PlanExecute")
	}
	if p.HasExec(GoalLoop) {
		parts = append(parts, "GoalLoop")
	}
	if p.HasExec(Sandboxed) {
		parts = append(parts, "Sandboxed")
	}
	if len(parts) == 0 {
		return "Exec: default"
	}
	return "Exec: " + strings.Join(parts, "+")
}

func toolExplain(p GoalExecProfile) string {
	var parts []string
	if p.HasTool(SequentialTools) {
		parts = append(parts, "Sequential")
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
		return "Tools: all"
	}
	return "Tools: " + strings.Join(parts, "+")
}

func verifyExplain(p GoalExecProfile) string {
	var parts []string
	if p.HasVerify(FastVerify) {
		parts = append(parts, "Fast")
	}
	if p.HasVerify(TestVerify) {
		parts = append(parts, "Test")
	}
	if p.HasVerify(IndependentReview) {
		parts = append(parts, "IndependentReview")
	}
	if p.HasVerify(DoubleReview) {
		parts = append(parts, "DoubleReview")
	}
	if p.HasVerify(ArtifactVerify) {
		parts = append(parts, "Artifact")
	}
	if len(parts) == 0 {
		return "Verify: default"
	}
	return "Verify: " + strings.Join(parts, "+")
}

func recoveryExplain(p GoalExecProfile) string {
	var parts []string
	if p.HasRecovery(Retry) {
		parts = append(parts, "Retry")
	}
	if p.HasRecovery(Replan) {
		parts = append(parts, "Replan")
	}
	if p.HasRecovery(ModelFallback) {
		parts = append(parts, "ModelFallback")
	}
	if p.HasRecovery(ContextReset) {
		parts = append(parts, "ContextReset")
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
	return "Recovery: " + strings.Join(parts, "+")
}
