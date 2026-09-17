package harness_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aether-dev/aether/internal/harness"
)

// ─── Composer tests ────────────────────────────────────────────────────────────

func TestComposerSmallBug(t *testing.T) {
	c := harness.NewComposer()
	p := c.Compose(harness.TaskAnalysis{
		TaskType:          "bug_fix",
		Complexity:        0.15,
		RiskLevel:         0.1,
		RepositoryFiles:   50,
		AffectedFiles:     2,
		EstimatedMinutes:  10,
		RequestedAutonomy: 0.5,
	})
	if !p.Has(harness.SelectiveContext) {
		t.Error("small bug: expected SelectiveContext")
	}
	if !p.HasExec(harness.Direct) {
		t.Error("small bug: expected Direct execution")
	}
	if !p.HasVerify(harness.TestVerify) {
		t.Error("small bug: expected TestVerify")
	}
	if !p.HasRecovery(harness.Retry) {
		t.Error("small bug: expected Retry recovery")
	}
	if p.HasExec(harness.PlanExecute) {
		t.Error("small bug: should not use PlanExecute")
	}
	t.Logf("small bug profile: %s", p.Explain())
}

func TestComposerLargeRefactor(t *testing.T) {
	c := harness.NewComposer()
	p := c.Compose(harness.TaskAnalysis{
		TaskType:          "refactor",
		Complexity:        0.85,
		RiskLevel:         0.65,
		RepositoryFiles:   3000,
		AffectedFiles:     80,
		EstimatedMinutes:  180,
		RequestedAutonomy: 0.8,
	})
	if !p.Has(harness.RepositoryMap) {
		t.Error("large refactor: expected RepositoryMap")
	}
	if !p.HasExec(harness.PlanExecute) {
		t.Error("large refactor: expected PlanExecute")
	}
	if !p.HasVerify(harness.IndependentReview) {
		t.Error("large refactor: expected IndependentReview")
	}
	if !p.HasRecovery(harness.CheckpointResume) {
		t.Error("large refactor: expected CheckpointResume")
	}
	t.Logf("large refactor profile: %s", p.Explain())
}

func TestComposerHardLongTask(t *testing.T) {
	c := harness.NewComposer()
	p := c.Compose(harness.TaskAnalysis{
		TaskType:          "feature",
		Complexity:        0.95,
		RiskLevel:         0.8,
		RepositoryFiles:   8000,
		AffectedFiles:     200,
		EstimatedMinutes:  600,
		RequestedAutonomy: 0.9,
	})
	if !p.Has(harness.SelectiveContext) || !p.Has(harness.RepositoryMap) {
		t.Error("hard long task: expected SelectiveContext + RepositoryMap together")
	}
	if !p.HasExec(harness.GoalLoop) {
		t.Error("hard long task: expected GoalLoop")
	}
	if !p.HasVerify(harness.ArtifactVerify) {
		t.Error("hard long task: expected ArtifactVerify")
	}
	if !p.HasRecovery(harness.ModelFallback) {
		t.Error("hard long task: expected ModelFallback")
	}
	if !p.HasRecovery(harness.ContextReset) {
		t.Error("hard long task: expected ContextReset")
	}
	t.Logf("hard long task profile: %s", p.Explain())
}

func TestComposerResearch(t *testing.T) {
	c := harness.NewComposer()
	p := c.Compose(harness.TaskAnalysis{
		TaskType:          "research",
		Complexity:        0.4,
		RiskLevel:         0.1,
		RepositoryFiles:   100,
		RequestedAutonomy: 0.6,
	})
	if !p.HasTool(harness.ResearchTools) {
		t.Error("research: expected ResearchTools")
	}
	t.Logf("research profile: %s", p.Explain())
}

// Different task types must produce different profiles.
func TestComposerDifferentiates(t *testing.T) {
	c := harness.NewComposer()
	bug := c.Compose(harness.TaskAnalysis{TaskType: "bug_fix", Complexity: 0.1, RiskLevel: 0.1})
	refactor := c.Compose(harness.TaskAnalysis{TaskType: "refactor", Complexity: 0.9, RiskLevel: 0.7, RepositoryFiles: 5000})
	if bug.Context == refactor.Context && bug.Execution == refactor.Execution {
		t.Error("composer should produce different profiles for different task types")
	}
}

// ─── Tool policy tests ─────────────────────────────────────────────────────────

func TestToolPolicyConcurrency(t *testing.T) {
	var p harness.GoalExecProfile
	p.AddTools(harness.SequentialTools)
	if p.EffectiveToolConcurrency() != 1 {
		t.Errorf("SequentialTools: want concurrency 1, got %d", p.EffectiveToolConcurrency())
	}

	var p2 harness.GoalExecProfile
	p2.AddTools(harness.ParallelTools)
	if p2.EffectiveToolConcurrency() < 2 {
		t.Errorf("ParallelTools: want concurrency >= 2, got %d", p2.EffectiveToolConcurrency())
	}
}

// ─── MaxTurns tests ────────────────────────────────────────────────────────────

func TestMaxTurnsScalesWithProfile(t *testing.T) {
	simple := harness.SmallBugProfile()
	complex := harness.HardLongTaskProfile()

	simpleTurns := harness.MaxTurnsFromProfile(simple)
	complexTurns := harness.MaxTurnsFromProfile(complex)

	if complexTurns <= simpleTurns {
		t.Errorf("complex profile should get more turns than simple: %d <= %d", complexTurns, simpleTurns)
	}
}

// ─── SystemExtra tests ─────────────────────────────────────────────────────────

func TestSystemExtraFromProfile(t *testing.T) {
	var p harness.GoalExecProfile
	p.AddExecution(harness.PlanExecute)
	p.AddTools(harness.CodingTools)
	p.AddVerify(harness.TestVerify)

	extra := harness.SystemExtraFromProfile(p)
	if extra == "" {
		t.Error("expected non-empty system extra for PlanExecute+CodingTools+TestVerify")
	}
	if !containsStr(extra, "plan") && !containsStr(extra, "Plan") {
		t.Errorf("expected 'plan' hint in system extra, got: %s", extra)
	}
}

// ─── StuckDetector tests ───────────────────────────────────────────────────────

func TestStuckDetectorRepeatedErrors(t *testing.T) {
	sd := harness.NewStuckDetector()
	err := "compilation failed: undefined symbol"
	w := harness.ObservationWindow{
		Errors:  []string{err, err, err},
	}
	signals := sd.Detect(w)
	if len(signals) == 0 {
		t.Error("stuck detector should fire after 3 repeated errors")
	}
}

func TestStuckDetectorRepeatedTestFailures(t *testing.T) {
	sd := harness.NewStuckDetector()
	w := harness.ObservationWindow{
		FailedGates: []string{"TestFoo", "TestBar", "TestFoo", "TestBar"},
	}
	signals := sd.Detect(w)
	if len(signals) == 0 {
		t.Error("stuck detector should fire on repeated test failures")
	}
}

func TestStuckDetectorOscillatingFiles(t *testing.T) {
	sd := harness.NewStuckDetector()
	// same file across 4 iterations
	w := harness.ObservationWindow{
		FilesModified: [][]string{
			{"internal/foo/bar.go"},
			{"internal/foo/bar.go"},
			{"internal/foo/bar.go"},
			{"internal/foo/bar.go"},
		},
	}
	signals := sd.Detect(w)
	if len(signals) == 0 {
		t.Error("stuck detector should fire on oscillating file edits")
	}
}

func TestStuckDetectorNoFalsePositive(t *testing.T) {
	sd := harness.NewStuckDetector()
	w := harness.ObservationWindow{
		Errors: []string{"err1"},
	}
	signals := sd.Detect(w)
	if len(signals) > 0 {
		t.Errorf("stuck detector fired false positive on single error: %v", signals)
	}
}

// ─── SuggestMutation tests ─────────────────────────────────────────────────────

func TestSuggestMutationDirectToPlanExecute(t *testing.T) {
	sd := harness.NewStuckDetector()
	p := harness.SmallBugProfile() // has Direct

	next, reason := sd.SuggestMutation(p, []harness.StuckSignal{harness.StuckRepeatedError})
	if reason == "" {
		t.Error("SuggestMutation should return a reason for StuckRepeatedError")
	}
	if !next.HasExec(harness.PlanExecute) {
		t.Errorf("Direct should upgrade to PlanExecute on StuckRepeatedError; got exec=%b", next.Execution)
	}
	t.Logf("mutation reason: %s", reason)
}

func TestSuggestMutationAddsIndependentReview(t *testing.T) {
	sd := harness.NewStuckDetector()
	var p harness.GoalExecProfile
	p.AddExecution(harness.PlanExecute)
	p.AddVerify(harness.TestVerify)
	p.AddRecovery(harness.Retry)

	next, reason := sd.SuggestMutation(p, []harness.StuckSignal{harness.StuckRepeatedTestFail})
	if reason == "" {
		t.Error("SuggestMutation should return a reason")
	}
	if !next.HasVerify(harness.IndependentReview) {
		t.Error("repeated test failures should add IndependentReview")
	}
}

// ─── Kernel mutation tests ─────────────────────────────────────────────────────

func TestKernelMutatesOnStuck(t *testing.T) {
	p := harness.SmallBugProfile()
	k := harness.NewKernel(harness.KernelOpts{
		GoalID:        "test-goal",
		CardID:        "test-card",
		Title:         "test",
		MaxIterations: 20,
		Profile:       p,
		Checkpoints:   harness.NewCheckpointStore(),
	})

	repeatedErr := "undefined: someSymbol"
	for i := 1; i <= 4; i++ {
		k.ObserveIteration(i, []string{repeatedErr}, nil, nil, nil, "")
	}

	mutations := k.Mutations()
	if len(mutations) == 0 {
		t.Error("kernel should have recorded at least one mutation after 4 identical errors")
	}
	last := mutations[len(mutations)-1]
	if last.Timestamp.IsZero() {
		t.Error("mutation timestamp must not be zero")
	}
	if last.Reason == "" {
		t.Error("mutation must have a reason")
	}
	if !last.NewProfile.HasExec(harness.PlanExecute) {
		t.Errorf("after repeated errors Direct should upgrade to PlanExecute; got exec=%b", last.NewProfile.Execution)
	}
	t.Logf("mutation[0]: %s", last.Reason)
}

func TestKernelMutationTracedWithTimestamp(t *testing.T) {
	k := harness.NewKernel(harness.KernelOpts{
		GoalID:        "ts-goal",
		MaxIterations: 10,
		Profile:       harness.SmallBugProfile(),
		Checkpoints:   harness.NewCheckpointStore(),
	})
	before := time.Now()
	for i := 1; i <= 4; i++ {
		k.ObserveIteration(i, []string{"same error"}, nil, nil, nil, "")
	}
	after := time.Now()
	for _, m := range k.Mutations() {
		if m.Timestamp.Before(before) || m.Timestamp.After(after) {
			t.Errorf("mutation timestamp out of range: %v", m.Timestamp)
		}
	}
}

// ─── Kernel iteration config tests ────────────────────────────────────────────

func TestKernelResolveIterationGoalLoop(t *testing.T) {
	var p harness.GoalExecProfile
	p.AddExecution(harness.GoalLoop)
	p.AddVerify(harness.TestVerify)
	p.AddTools(harness.CodingTools)

	k := harness.NewKernel(harness.KernelOpts{
		GoalID:        "gi",
		MaxIterations: 8,
		Profile:       p,
		Checkpoints:   harness.NewCheckpointStore(),
	})
	cfg := k.ResolveIteration(1)
	if cfg.MaxTurns < 8 {
		t.Errorf("GoalLoop should yield at least 8 turns per iteration; got %d", cfg.MaxTurns)
	}
	if !cfg.VerifyCfg.RunQualityGates {
		t.Error("TestVerify should set RunQualityGates=true")
	}
}

// ─── Checkpoint tests ─────────────────────────────────────────────────────────

func TestCheckpointStoreRoundTrip(t *testing.T) {
	cs := harness.NewCheckpointStore()
	ctx := context.Background()
	cp := harness.Checkpoint{
		GoalID:      "goal-1",
		Iteration:   3,
		ProfileVer:  1,
		WorkDir:     "/tmp/work",
		LastSummary: "did something",
		SavedAt:     time.Now().UTC(),
	}
	cs.Save(ctx, cp)
	loaded, ok := cs.Load(ctx, "goal-1")
	if !ok {
		t.Fatal("checkpoint not found after save")
	}
	if loaded.Iteration != 3 {
		t.Errorf("iteration mismatch: want 3, got %d", loaded.Iteration)
	}
	if loaded.WorkDir != "/tmp/work" {
		t.Errorf("workdir mismatch: want /tmp/work, got %s", loaded.WorkDir)
	}
	cs.Delete(ctx, "goal-1")
	if _, ok := cs.Load(ctx, "goal-1"); ok {
		t.Error("checkpoint should be gone after delete")
	}
}

// ─── Profile explain tests ─────────────────────────────────────────────────────

func TestProfileExplain(t *testing.T) {
	p := harness.SmallBugProfile()
	exp := p.Explain()
	if exp == "" {
		t.Error("Explain() should not be empty")
	}
	t.Logf("explain: %s", exp)
}

func TestPresetProfilesAreDistinct(t *testing.T) {
	small := harness.SmallBugProfile()
	large := harness.LargeRefactorProfile()
	hard := harness.HardLongTaskProfile()
	if small.Explain() == large.Explain() {
		t.Error("small bug and large refactor should differ")
	}
	if large.Explain() == hard.Explain() {
		t.Error("large refactor and hard long task should differ")
	}
	if !small.HasExec(harness.Direct) {
		t.Error("small bug preset: Direct")
	}
	if !large.HasExec(harness.PlanExecute) {
		t.Error("large refactor preset: PlanExecute")
	}
	if !hard.HasRecovery(harness.ModelFallback) && !hard.HasVerify(harness.ArtifactVerify) {
		t.Error("hard long task should carry extra verify or fallback")
	}
}

func TestUnmarshalProfileRoundTrip(t *testing.T) {
	p := harness.SmallBugProfile()
	hpIn := harness.HarnessProfile{Current: p}
	b, err := json.Marshal(hpIn)
	if err != nil {
		t.Fatal(err)
	}
	var hp harness.HarnessProfile
	if err := harness.UnmarshalProfile(string(b), &hp); err != nil {
		t.Fatal(err)
	}
	back := hp.Current
	if !back.Has(harness.SelectiveContext) {
		t.Error("round-trip lost SelectiveContext")
	}
	if !back.HasExec(harness.Direct) {
		t.Error("round-trip lost Direct")
	}
}

func TestAddHelpersAreComposable(t *testing.T) {
	var p harness.GoalExecProfile
	p.AddContext(harness.SelectiveContext)
	p.AddContext(harness.RepositoryMap)
	p.AddExecution(harness.Direct)
	p.AddExecution(harness.GoalLoop)
	if !p.Has(harness.SelectiveContext) || !p.Has(harness.RepositoryMap) {
		t.Error("context flags should compose")
	}
	if !p.HasExec(harness.Direct) || !p.HasExec(harness.GoalLoop) {
		t.Error("execution flags should compose")
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}