package harness

import (
	"encoding/json"
	"strings"
	"time"
)

// HarnessMutation records one profile change during execution.
type HarnessMutation struct {
	GoalID     string          `json:"goalId"`
	Iteration  int             `json:"iteration"`
	Reason     string          `json:"reason"`
	OldProfile GoalExecProfile `json:"oldProfile"`
	NewProfile GoalExecProfile `json:"newProfile"`
	Timestamp  time.Time       `json:"timestamp"`
	// Result is filled in after the mutation's effect is observed.
	Result string `json:"result,omitempty"`
}

// HarnessProfile is the full harness state attached to a Goal or Card.
// It combines the current execution profile with the mutation trace.
type HarnessProfile struct {
	GoalID    string          `json:"goalId"`
	CardID    string          `json:"cardId"`
	Current   GoalExecProfile `json:"current"`
	Mutations []HarnessMutation `json:"mutations,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// Mutate applies a profile change and appends it to the mutation trace.
// The new profile's Version is incremented automatically.
func (hp *HarnessProfile) Mutate(newProfile GoalExecProfile, reason string, iteration int) {
	old := hp.Current
	newProfile.Version = old.Version + 1
	hp.Mutations = append(hp.Mutations, HarnessMutation{
		GoalID:     hp.GoalID,
		Iteration:  iteration,
		Reason:     reason,
		OldProfile: old,
		NewProfile: newProfile,
		Timestamp:  time.Now().UTC(),
	})
	hp.Current = newProfile
	hp.UpdatedAt = time.Now().UTC()
}

// JSON serialises the profile for storage.
func (hp *HarnessProfile) MarshalJSON() ([]byte, error) {
	type alias HarnessProfile
	return json.Marshal((*alias)(hp))
}

// ── Stuck detector ────────────────────────────────────────────────────────────

// StuckSignal identifies why the executor appears stuck.
type StuckSignal string

const (
	StuckRepeatedError    StuckSignal = "repeated_error"
	StuckRepeatedTestFail StuckSignal = "repeated_test_fail"
	StuckFileOscillation  StuckSignal = "file_oscillation"
	StuckNoProgress       StuckSignal = "no_progress"
	StuckRepeatedTools    StuckSignal = "repeated_tools"
	StuckPlanStale        StuckSignal = "plan_stale"
)

// ObservationWindow is the history the stuck detector analyses.
type ObservationWindow struct {
	// Last N iteration error messages.
	Errors []string
	// Last N gate failure names.
	FailedGates []string
	// Files modified per iteration (index = iteration).
	FilesModified [][]string
	// Tool calls per iteration.
	ToolCalls [][]string
	// Summary texts per iteration.
	Summaries []string
}

// StuckDetector analyses an observation window and returns signals.
type StuckDetector struct {
	// RepeatThreshold is how many times the same thing must occur to count as stuck.
	RepeatThreshold int
}

func NewStuckDetector() *StuckDetector {
	return &StuckDetector{RepeatThreshold: 2}
}

// Detect analyses the window and returns any detected stuck signals.
func (sd *StuckDetector) Detect(w ObservationWindow) []StuckSignal {
	var signals []StuckSignal
	thresh := sd.RepeatThreshold
	if thresh <= 0 {
		thresh = 2
	}

	// Repeated errors.
	if counts := countFrequency(w.Errors); maxCount(counts) >= thresh {
		signals = append(signals, StuckRepeatedError)
	}

	// Repeated gate failures.
	if counts := countFrequency(w.FailedGates); maxCount(counts) >= thresh {
		signals = append(signals, StuckRepeatedTestFail)
	}

	// File oscillation: same file appears modified across multiple iterations
	// and then reverted (appears in both even and odd iterations).
	if detectOscillation(w.FilesModified, thresh) {
		signals = append(signals, StuckFileOscillation)
	}

	// No progress: all summaries are identical or near-identical.
	if len(w.Summaries) >= thresh && allSimilar(w.Summaries, thresh) {
		signals = append(signals, StuckNoProgress)
	}

	// Repeated tool call sequences.
	if detectRepeatedToolSeq(w.ToolCalls, thresh) {
		signals = append(signals, StuckRepeatedTools)
	}

	return signals
}

// SuggestMutation returns the recommended profile mutation given stuck signals.
func (sd *StuckDetector) SuggestMutation(current GoalExecProfile, signals []StuckSignal) (GoalExecProfile, string) {
	if len(signals) == 0 {
		return current, ""
	}

	next := current
	var reasons []string

	for _, sig := range signals {
		switch sig {
		case StuckRepeatedError:
			// Direct → PlanExecute
			if next.HasExec(Direct) {
				next.Execution &^= Direct
				next.Execution |= PlanExecute
				reasons = append(reasons, "Direct→PlanExecute: repeated errors suggest unstructured execution")
			} else if !next.HasRecovery(Replan) {
				next.Recovery |= Replan
				reasons = append(reasons, "added Replan: repeated errors require a new plan")
			}

		case StuckRepeatedTestFail:
			// Add independent review to get fresh eyes.
			if !next.HasVerify(IndependentReview) {
				next.Verify |= IndependentReview
				reasons = append(reasons, "added IndependentReview: repeated test failures")
			}
			// Add RepositoryMap so reviewer has full picture.
			if !next.Has(RepositoryMap) {
				next.Context |= RepositoryMap
				reasons = append(reasons, "added RepositoryMap: tests failing, reviewer needs full context")
			}

		case StuckFileOscillation:
			// Add ModelFallback — current model is going in circles.
			if !next.HasRecovery(ModelFallback) {
				next.Recovery |= ModelFallback
				reasons = append(reasons, "added ModelFallback: file oscillation detected")
			}
			// Also add FreshContext.
			if !next.Has(FreshContext) {
				next.Context |= FreshContext
				reasons = append(reasons, "added FreshContext: clear contaminated context")
			}

		case StuckNoProgress:
			// SelectiveContext → RepositoryMap, escalate context.
			if !next.Has(RepositoryMap) {
				next.Context |= RepositoryMap
				reasons = append(reasons, "added RepositoryMap: no progress, need broader view")
			}
			if !next.Has(LargeContext) {
				next.Context |= LargeContext
				reasons = append(reasons, "added LargeContext: expand context budget")
			}

		case StuckRepeatedTools:
			// Sequential → Parallel or vice-versa.
			if next.HasTool(SequentialTools) && !next.HasTool(ParallelTools) {
				next.Tools |= ParallelTools
				reasons = append(reasons, "added ParallelTools: repeated sequential tool calls")
			}
			// Also force Replan.
			if !next.HasRecovery(Replan) {
				next.Recovery |= Replan
				reasons = append(reasons, "added Replan: repeated tool call pattern suggests stale plan")
			}

		case StuckPlanStale:
			next.Recovery |= Replan
			if !next.Has(FreshContext) {
				next.Context |= FreshContext
			}
			reasons = append(reasons, "plan stale: Replan+FreshContext")
		}
	}

	if len(reasons) == 0 {
		return current, ""
	}
	return next, strings.Join(reasons, "; ")
}

// ── helpers ───────────────────────────────────────────────────────────────────

func countFrequency(items []string) map[string]int {
	m := map[string]int{}
	for _, v := range items {
		if v != "" {
			m[v]++
		}
	}
	return m
}

func maxCount(m map[string]int) int {
	max := 0
	for _, v := range m {
		if v > max {
			max = v
		}
	}
	return max
}

func detectOscillation(filesByIteration [][]string, thresh int) bool {
	if len(filesByIteration) < thresh*2 {
		return false
	}
	// File must appear in at least thresh different iterations.
	counts := map[string]int{}
	for _, files := range filesByIteration {
		seen := map[string]bool{}
		for _, f := range files {
			if !seen[f] {
				counts[f]++
				seen[f] = true
			}
		}
	}
	return maxCount(counts) >= thresh*2
}

func allSimilar(summaries []string, thresh int) bool {
	if len(summaries) < thresh {
		return false
	}
	base := normalise(summaries[0])
	same := 0
	for _, s := range summaries[1:] {
		if normalise(s) == base {
			same++
		}
	}
	return same >= thresh-1
}

func normalise(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func detectRepeatedToolSeq(toolsByIter [][]string, thresh int) bool {
	if len(toolsByIter) < thresh {
		return false
	}
	type seq struct{ a, b string }
	counts := map[seq]int{}
	for _, calls := range toolsByIter {
		for i := 0; i+1 < len(calls); i++ {
			counts[seq{calls[i], calls[i+1]}]++
		}
	}
	max := 0
	for _, v := range counts {
		if v > max {
			max = v
		}
	}
	return max >= thresh
}