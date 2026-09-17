package harness

import (
	"encoding/json"
	"strings"
)

// SystemExtraFromProfile builds a system-prompt addendum describing the active
// harness policy so the agent can be aware of its execution constraints.
func SystemExtraFromProfile(p GoalExecProfile) string {
	var parts []string

	// Context hints.
	if p.Has(SelectiveContext) {
		parts = append(parts, "Focus only on files directly relevant to the task.")
	}
	if p.Has(RepositoryMap) {
		parts = append(parts, "A repository map is provided — use it to navigate the codebase.")
	}
	if p.Has(FreshContext) {
		parts = append(parts, "Your context budget is limited — be concise and targeted.")
	}
	if p.Has(MemoryHeavy) {
		parts = append(parts, "Consult memory entries before starting new work.")
	}

	// Execution hints.
	if p.HasExec(PlanExecute) {
		parts = append(parts, "Write an explicit plan before taking actions.")
	}
	if p.HasExec(GoalLoop) {
		parts = append(parts, "Continue iterating until the completion criteria are met.")
	}
	if p.HasExec(Sandboxed) {
		parts = append(parts, "Work only inside the provided workspace directory.")
	}

	// Tool hints.
	if p.HasTool(SequentialTools) {
		parts = append(parts, "Call one tool at a time; wait for each result before continuing.")
	}
	if p.HasTool(ParallelTools) {
		parts = append(parts, "You may call independent tools in parallel to save time.")
	}
	if p.HasTool(RestrictedTools) {
		parts = append(parts, "Only use the explicitly listed tools; avoid other capabilities.")
	}
	if p.HasTool(CodingTools) {
		parts = append(parts, "Primary tools: read_file, write_file, patch_file, run_command.")
	}
	if p.HasTool(ResearchTools) {
		parts = append(parts, "Primary tools: web_search, read_file, summarize.")
	}

	// Verification hints.
	if p.HasVerify(TestVerify) {
		parts = append(parts, "Run tests after each significant change.")
	}
	if p.HasVerify(ArtifactVerify) {
		parts = append(parts, "Produce verifiable artifacts: diffs, build logs, screenshots.")
	}

	if len(parts) == 0 {
		return ""
	}
	return "Execution policy:\n" + strings.Join(parts, "\n")
}

// MaxTurnsFromProfile derives an appropriate agent turn cap from the profile.
func MaxTurnsFromProfile(p GoalExecProfile) int {
	base := 12
	if p.HasExec(GoalLoop) {
		base += 8
	}
	if p.HasExec(PlanExecute) {
		base += 4
	}
	if p.HasExec(Parallel) {
		base += 4
	}
	if p.HasVerify(IndependentReview) {
		base += 4
	}
	if p.HasVerify(ArtifactVerify) {
		base += 2
	}
	if p.HasExec(Sandboxed) && !p.HasExec(GoalLoop) {
		base -= 4
	}
	if base < 4 {
		base = 4
	}
	return base
}

// UnmarshalProfile decodes a JSON harness profile.
func UnmarshalProfile(raw string, hp *HarnessProfile) error {
	return json.Unmarshal([]byte(raw), hp)
}