package harness

import (
	"context"
	"strings"
	"time"
)

// VerifyConfig is the resolved verification plan for a goal iteration.
type VerifyConfig struct {
	// RunQualityGates signals the kernel to invoke quality gate runner.
	RunQualityGates bool
	// RequestIndependentReview signals spawning a fresh-context review agent.
	RequestIndependentReview bool
	// RequireDoubleReview requires two independent reviewers to agree.
	RequireDoubleReview bool
	// RequireArtifacts means evidence (diff/log/build output) must exist.
	RequireArtifacts bool
	// Explanation describes the verification plan.
	Explanation string
}

// VerifyStrategy resolves the verification configuration from a profile.
type VerifyStrategy struct {
	Profile GoalExecProfile
}

// Resolve returns the VerifyConfig for the current profile.
func (vs *VerifyStrategy) Resolve() VerifyConfig {
	var reasons []string
	vc := VerifyConfig{}

	if vs.Profile.HasVerify(FastVerify) {
		// Minimal — no gates, no review.
		reasons = append(reasons, "fast verify: syntax check only")
	}
	if vs.Profile.HasVerify(TestVerify) {
		vc.RunQualityGates = true
		reasons = append(reasons, "test verify: quality gates enabled")
	}
	if vs.Profile.HasVerify(IndependentReview) {
		vc.RequestIndependentReview = true
		reasons = append(reasons, "independent review: fresh-context reviewer agent")
	}
	if vs.Profile.HasVerify(DoubleReview) {
		vc.RequestIndependentReview = true
		vc.RequireDoubleReview = true
		reasons = append(reasons, "double review: two reviewers must agree")
	}
	if vs.Profile.HasVerify(ArtifactVerify) {
		vc.RequireArtifacts = true
		reasons = append(reasons, "artifact verify: diff/log/build evidence required")
	}
	if len(reasons) == 0 {
		vc.RunQualityGates = true
		reasons = append(reasons, "default: quality gates")
	}
	vc.Explanation = strings.Join(reasons, "; ")
	return vc
}

// ── Recovery ─────────────────────────────────────────────────────────────────

// RecoveryAction is what the kernel should do after a failure.
type RecoveryAction string

const (
	RecoveryRetry           RecoveryAction = "retry"
	RecoveryReplan          RecoveryAction = "replan"
	RecoveryModelFallback   RecoveryAction = "model_fallback"
	RecoveryContextReset    RecoveryAction = "context_reset"
	RecoveryCheckpointResume RecoveryAction = "checkpoint_resume"
	RecoveryEscalate        RecoveryAction = "escalate"
	RecoveryGiveUp          RecoveryAction = "give_up"
)

// RecoveryPlan is the ordered list of actions to attempt on failure.
type RecoveryPlan struct {
	Actions     []RecoveryAction
	MaxRetries  int
	Explanation string
}

// RecoveryStrategy resolves the recovery plan from a profile.
type RecoveryStrategy struct {
	Profile GoalExecProfile
}

// Resolve returns the ordered RecoveryPlan for the current profile.
// Order matters: cheaper/faster recovery is attempted before expensive.
func (rs *RecoveryStrategy) Resolve() RecoveryPlan {
	var actions []RecoveryAction
	var reasons []string

	// Order: Retry → CheckpointResume → Replan → ContextReset → ModelFallback → Escalation
	if rs.Profile.HasRecovery(Retry) {
		actions = append(actions, RecoveryRetry)
		reasons = append(reasons, "retry transient failures")
	}
	if rs.Profile.HasRecovery(CheckpointResume) {
		actions = append(actions, RecoveryCheckpointResume)
		reasons = append(reasons, "resume from checkpoint")
	}
	if rs.Profile.HasRecovery(Replan) {
		actions = append(actions, RecoveryReplan)
		reasons = append(reasons, "replan on structural failure")
	}
	if rs.Profile.HasRecovery(ContextReset) {
		actions = append(actions, RecoveryContextReset)
		reasons = append(reasons, "context reset + fresh handoff")
	}
	if rs.Profile.HasRecovery(ModelFallback) {
		actions = append(actions, RecoveryModelFallback)
		reasons = append(reasons, "model fallback from checkpoint")
	}
	if rs.Profile.HasRecovery(Escalation) {
		actions = append(actions, RecoveryEscalate)
		reasons = append(reasons, "escalate to human")
	}
	if len(actions) == 0 {
		actions = append(actions, RecoveryGiveUp)
		reasons = append(reasons, "no recovery policy configured")
	}

	maxRetries := 3
	if rs.Profile.HasRecovery(Retry) && !rs.Profile.HasRecovery(Replan) {
		maxRetries = 5 // more retries when replan is not available
	}

	return RecoveryPlan{
		Actions:     actions,
		MaxRetries:  maxRetries,
		Explanation: strings.Join(reasons, "; "),
	}
}

// ── Checkpoint ───────────────────────────────────────────────────────────────

// Checkpoint records execution state for resume-after-restart.
type Checkpoint struct {
	GoalID      string        `json:"goalId"`
	CardID      string        `json:"cardId"`
	Iteration   int           `json:"iteration"`
	ProfileVer  int           `json:"profileVersion"`
	WorkDir     string        `json:"workDir"`
	LastSummary string        `json:"lastSummary"`
	SavedAt     time.Time     `json:"savedAt"`
}

// CheckpointStore is a simple in-process checkpoint registry.
// In production this is persisted via the store's KV layer.
type CheckpointStore struct {
	data map[string]Checkpoint
}

func NewCheckpointStore() *CheckpointStore {
	return &CheckpointStore{data: map[string]Checkpoint{}}
}

func (cs *CheckpointStore) Save(_ context.Context, cp Checkpoint) {
	cs.data[cp.GoalID] = cp
}

func (cs *CheckpointStore) Load(_ context.Context, goalID string) (Checkpoint, bool) {
	cp, ok := cs.data[goalID]
	return cp, ok
}

func (cs *CheckpointStore) Delete(_ context.Context, goalID string) {
	delete(cs.data, goalID)
}