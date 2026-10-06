package harness

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// VerifyConfig is the resolved verification plan for a goal iteration.
type VerifyConfig struct {
	// RunQualityGates runs the contract's gates every iteration; otherwise
	// they run only once the reviewer finds the criteria met.
	RunQualityGates bool
	// RequestIndependentReview: the reviewer reads the diff itself.
	RequestIndependentReview bool
	// RequireDoubleReview requires two independent reviewers to agree.
	RequireDoubleReview bool
	// RequireArtifacts means done needs changed files in the workspace.
	RequireArtifacts bool
	// Explanation describes the verification plan.
	Explanation string
}

// VerifyStrategy resolves the verification configuration from a profile.
type VerifyStrategy struct {
	Profile GoalExecProfile
}

// Resolve returns the VerifyConfig for the current profile. Every goal is
// reviewed by a model reviewer; the tactics decide how much it sees and how
// often the gates run.
func (vs *VerifyStrategy) Resolve() VerifyConfig {
	var reasons []string
	vc := VerifyConfig{RunQualityGates: true}

	if vs.Profile.HasVerify(FastVerify) && !vs.Profile.HasVerify(TestVerify) {
		vc.RunQualityGates = false
		reasons = append(reasons, "fast verify: gates only once the reviewer is satisfied")
	} else {
		reasons = append(reasons, "gates every iteration")
	}
	if vs.Profile.HasVerify(IndependentReview) {
		vc.RequestIndependentReview = true
		reasons = append(reasons, "independent review: the reviewer reads the diff")
	}
	if vs.Profile.HasVerify(DoubleReview) {
		vc.RequestIndependentReview = true
		vc.RequireDoubleReview = true
		reasons = append(reasons, "double review: two reviewers must agree")
	}
	if vs.Profile.HasVerify(ArtifactVerify) {
		vc.RequireArtifacts = true
		reasons = append(reasons, "artifact verify: done needs changed files")
	}
	vc.Explanation = strings.Join(reasons, "; ")
	return vc
}

// ── Recovery ─────────────────────────────────────────────────────────────────

// RecoveryAction is what the kernel should do after a failure.
type RecoveryAction string

const (
	RecoveryRetry            RecoveryAction = "retry"
	RecoveryReplan           RecoveryAction = "replan"
	RecoveryModelFallback    RecoveryAction = "model_fallback"
	RecoveryContextReset     RecoveryAction = "context_reset"
	RecoveryCheckpointResume RecoveryAction = "checkpoint_resume"
	RecoveryEscalate         RecoveryAction = "escalate"
	RecoveryGiveUp           RecoveryAction = "give_up"
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
	GoalID      string `json:"goalId"`
	Iteration   int    `json:"iteration"`
	ProfileVer  int    `json:"profileVersion"`
	WorkDir     string `json:"workDir"`
	LastSummary string `json:"lastSummary"`
	// Progress holds a short line per finished iteration (clipped), the
	// hand-off a resumed or fresh-context run starts from.
	Progress []string  `json:"progress,omitempty"`
	SavedAt  time.Time `json:"savedAt"`
}

// CheckpointBackend persists checkpoints (the store's goal_checkpoints).
type CheckpointBackend interface {
	PutCheckpoint(ctx context.Context, goalID, body string) error
	GetCheckpoint(ctx context.Context, goalID string) (string, error)
	DeleteCheckpoint(ctx context.Context, goalID string) error
}

// CheckpointStore keeps each goal's latest checkpoint. It is shared by every
// running goal, so it is safe for concurrent use; with a backend the
// checkpoint also survives a daemon restart.
type CheckpointStore struct {
	mu      sync.Mutex
	data    map[string]Checkpoint
	backend CheckpointBackend
}

// NewCheckpointStoreWith also writes every checkpoint through to backend.
func NewCheckpointStoreWith(backend CheckpointBackend) *CheckpointStore {
	return &CheckpointStore{data: map[string]Checkpoint{}, backend: backend}
}

func (cs *CheckpointStore) Save(ctx context.Context, cp Checkpoint) {
	cs.mu.Lock()
	cs.data[cp.GoalID] = cp
	cs.mu.Unlock()
	if cs.backend != nil {
		if b, err := json.Marshal(cp); err == nil {
			_ = cs.backend.PutCheckpoint(ctx, cp.GoalID, string(b))
		}
	}
}

func (cs *CheckpointStore) Load(ctx context.Context, goalID string) (Checkpoint, bool) {
	cs.mu.Lock()
	cp, ok := cs.data[goalID]
	cs.mu.Unlock()
	if ok || cs.backend == nil {
		return cp, ok
	}
	body, err := cs.backend.GetCheckpoint(ctx, goalID)
	if err != nil || json.Unmarshal([]byte(body), &cp) != nil {
		return Checkpoint{}, false
	}
	return cp, true
}

func (cs *CheckpointStore) Delete(ctx context.Context, goalID string) {
	cs.mu.Lock()
	delete(cs.data, goalID)
	cs.mu.Unlock()
	if cs.backend != nil {
		_ = cs.backend.DeleteCheckpoint(ctx, goalID)
	}
}
