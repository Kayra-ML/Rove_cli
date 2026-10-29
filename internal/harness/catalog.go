package harness

import (
	"fmt"
	"strings"
)

// Tactic is one flag of a category, as the app shows and picks it.
type Tactic struct {
	Key string `json:"key"`
	Bit uint32 `json:"bit"`
}

// Category is one of a profile's five bitmasks and the tactics it offers.
type Category struct {
	Key     string   `json:"key"`
	Tactics []Tactic `json:"tactics"`
}

// Catalog lists the tactics a profile can be made of, by category, in the
// order the app shows them. Retired flags (Parallel, Delegated) still
// decode in stored profiles but are not offered.
func Catalog() []Category {
	return []Category{
		{Key: "context", Tactics: []Tactic{
			{"selective", uint32(SelectiveContext)},
			{"large", uint32(LargeContext)},
			{"fresh", uint32(FreshContext)},
			{"repoMap", uint32(RepositoryMap)},
			{"memory", uint32(MemoryHeavy)},
		}},
		{Key: "execution", Tactics: []Tactic{
			{"direct", uint32(Direct)},
			{"planExecute", uint32(PlanExecute)},
			{"goalLoop", uint32(GoalLoop)},
			{"sandboxed", uint32(Sandboxed)},
		}},
		{Key: "tools", Tactics: []Tactic{
			{"sequential", uint32(SequentialTools)},
			{"parallel", uint32(ParallelTools)},
			{"restricted", uint32(RestrictedTools)},
			{"coding", uint32(CodingTools)},
			{"research", uint32(ResearchTools)},
		}},
		{Key: "verify", Tactics: []Tactic{
			{"fast", uint32(FastVerify)},
			{"test", uint32(TestVerify)},
			{"independentReview", uint32(IndependentReview)},
			{"doubleReview", uint32(DoubleReview)},
			{"artifact", uint32(ArtifactVerify)},
		}},
		{Key: "recovery", Tactics: []Tactic{
			{"retry", uint32(Retry)},
			{"replan", uint32(Replan)},
			{"modelFallback", uint32(ModelFallback)},
			{"contextReset", uint32(ContextReset)},
			{"checkpointResume", uint32(CheckpointResume)},
			{"escalation", uint32(Escalation)},
		}},
	}
}

// retiredExecution are execution flags nothing sets or reads any more.
const retiredExecution = Parallel | Delegated

// Clean is a profile picked by hand made fit to run: retired flags are
// dropped, and a flag no tactic has is an error (a client out of step with
// this daemon). Any mix of known tactics runs: the kernel reads each flag on
// its own, and an empty category means its default.
func (p GoalExecProfile) Clean() (GoalExecProfile, error) {
	p.Execution &^= retiredExecution
	masks := map[string]uint32{}
	for _, c := range Catalog() {
		for _, t := range c.Tactics {
			masks[c.Key] |= t.Bit
		}
	}
	var bad []string
	for key, v := range map[string]uint32{
		"context": uint32(p.Context), "execution": uint32(p.Execution), "tools": uint32(p.Tools),
		"verify": uint32(p.Verify), "recovery": uint32(p.Recovery),
	} {
		if extra := v &^ masks[key]; extra != 0 {
			bad = append(bad, fmt.Sprintf("%s %#x", key, extra))
		}
	}
	if len(bad) > 0 {
		return p, fmt.Errorf("unknown harness tactics: %s", strings.Join(bad, ", "))
	}
	if p.ContextBudgetTokens < 0 || p.MaxToolConcurrency < 0 {
		return p, fmt.Errorf("harness limits cannot be negative")
	}
	return p, nil
}

// Resolved is what a profile comes to when a goal runs: the limits and
// plans the kernel derives from its tactics, for people to read.
type Resolved struct {
	MaxTurns        int `json:"maxTurns"`
	ContextBudget   int `json:"contextBudget"`
	ToolConcurrency int `json:"toolConcurrency"`
	// AllowedTools is nil when the profile restricts nothing.
	AllowedTools []string `json:"allowedTools"`
	Tools        string   `json:"tools"`
	Verify       string   `json:"verify"`
	Recovery     []string `json:"recovery"`
	RecoveryNote string   `json:"recoveryNote"`
	MaxRetries   int      `json:"maxRetries"`
}

// Resolve derives what p does, with the tools the daemon has registered.
func Resolve(p GoalExecProfile, allTools []string) Resolved {
	tc := (&ToolPolicy{Profile: p, AllAvailableTools: allTools}).Resolve()
	vc := (&VerifyStrategy{Profile: p}).Resolve()
	rp := (&RecoveryStrategy{Profile: p}).Resolve()
	actions := make([]string, 0, len(rp.Actions))
	for _, a := range rp.Actions {
		actions = append(actions, string(a))
	}
	return Resolved{
		MaxTurns:        MaxTurnsFromProfile(p),
		ContextBudget:   p.EffectiveContextBudget(),
		ToolConcurrency: tc.MaxConcurrency,
		AllowedTools:    tc.AllowedTools,
		Tools:           tc.Explanation,
		Verify:          vc.Explanation,
		Recovery:        actions,
		RecoveryNote:    rp.Explanation,
		MaxRetries:      rp.MaxRetries,
	}
}
