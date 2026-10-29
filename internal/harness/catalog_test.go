package harness

import "testing"

// The catalog offers every live flag once, with the bit it really has.
func TestCatalogCoversEveryLiveFlag(t *testing.T) {
	want := map[string]uint32{
		"context":   uint32(SelectiveContext | LargeContext | FreshContext | RepositoryMap | MemoryHeavy),
		"execution": uint32(Direct | PlanExecute | GoalLoop | Sandboxed),
		"tools":     uint32(SequentialTools | ParallelTools | RestrictedTools | CodingTools | ResearchTools),
		"verify":    uint32(FastVerify | TestVerify | IndependentReview | DoubleReview | ArtifactVerify),
		"recovery":  uint32(Retry | Replan | ModelFallback | ContextReset | CheckpointResume | Escalation),
	}
	keys := map[string]bool{}
	for _, c := range Catalog() {
		var got uint32
		for _, tc := range c.Tactics {
			if got&tc.Bit != 0 || tc.Bit == 0 || tc.Bit&(tc.Bit-1) != 0 {
				t.Errorf("%s/%s: bit %#x is not a single new flag", c.Key, tc.Key, tc.Bit)
			}
			if keys[c.Key+"/"+tc.Key] {
				t.Errorf("%s/%s twice", c.Key, tc.Key)
			}
			keys[c.Key+"/"+tc.Key] = true
			got |= tc.Bit
		}
		if got != want[c.Key] {
			t.Errorf("%s offers %#x, want %#x", c.Key, got, want[c.Key])
		}
		delete(want, c.Key)
	}
	if len(want) != 0 {
		t.Errorf("categories missing: %v", want)
	}
}

func TestCleanDropsRetiredAndRefusesUnknown(t *testing.T) {
	p, err := GoalExecProfile{Execution: PlanExecute | Parallel | Delegated, Recovery: Retry}.Clean()
	if err != nil || p.Execution != PlanExecute {
		t.Fatalf("clean = %+v, %v", p, err)
	}
	if _, err := (GoalExecProfile{Verify: 1 << 9}).Clean(); err == nil {
		t.Fatal("an unknown verify flag was let through")
	}
	if _, err := (GoalExecProfile{MaxToolConcurrency: -1}).Clean(); err == nil {
		t.Fatal("a negative limit was let through")
	}
}

func TestResolveSaysWhatAProfileDoes(t *testing.T) {
	r := Resolve(LargeRefactorProfile(), []string{"read_file", "write_file", "patch_file", "list_dir", "shell", "git_status", "git_commit", "web_fetch"})
	if r.MaxTurns != MaxTurnsFromProfile(LargeRefactorProfile()) || r.ToolConcurrency != 8 {
		t.Fatalf("limits = %+v", r)
	}
	for _, n := range r.AllowedTools {
		if n == "web_fetch" {
			t.Fatal("coding tools let a web tool through")
		}
	}
	if len(r.Recovery) != 2 || r.Recovery[0] != string(RecoveryCheckpointResume) || r.Recovery[1] != string(RecoveryReplan) {
		t.Fatalf("recovery = %v", r.Recovery)
	}
	if open := Resolve(GoalExecProfile{}, nil); open.AllowedTools != nil || open.Recovery[0] != string(RecoveryGiveUp) {
		t.Fatalf("an empty profile resolves to %+v", open)
	}
}
