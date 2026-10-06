package harness

// NewCheckpointStore is a store that keeps checkpoints in memory only.
func NewCheckpointStore() *CheckpointStore {
	return &CheckpointStore{data: map[string]Checkpoint{}}
}

// LargeRefactorProfile is for large, cross-cutting refactors.
func LargeRefactorProfile() GoalExecProfile {
	return GoalExecProfile{
		Mode:           ProfileAuto,
		Context:        RepositoryMap | SelectiveContext,
		Execution:      PlanExecute | Sandboxed,
		Tools:          CodingTools | ParallelTools,
		Verify:         TestVerify | IndependentReview,
		Recovery:       CheckpointResume | Replan,
		ComposerReason: "large refactor: repo map, plan+execute, snapshot first, independent review, checkpoint resume",
	}
}

// HardLongTaskProfile is for complex, multi-day autonomous tasks.
func HardLongTaskProfile() GoalExecProfile {
	return GoalExecProfile{
		Mode:           ProfileAuto,
		Context:        SelectiveContext | RepositoryMap | FreshContext,
		Execution:      GoalLoop | PlanExecute,
		Tools:          CodingTools | ParallelTools,
		Verify:         IndependentReview | ArtifactVerify,
		Recovery:       Replan | ContextReset | ModelFallback | CheckpointResume,
		ComposerReason: "hard/long task: goal loop with a plan, fresh context, full verification, full recovery stack",
	}
}
