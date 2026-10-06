package harness

import "strings"

// ToolPolicy resolves which tools are exposed to the agent and
// the concurrency settings for tool execution.
type ToolPolicy struct {
	Profile GoalExecProfile
	// AllAvailableTools is the full list of registered tool names.
	AllAvailableTools []string
}

// ToolExecConfig is the resolved tool configuration.
type ToolExecConfig struct {
	// AllowedTools is the list of tool names the agent may call.
	AllowedTools []string
	// MaxConcurrency is how many tool calls may run in parallel (1 = sequential).
	MaxConcurrency int
	// Explanation describes why these tool choices were made.
	Explanation string
}

// Coding tools: the built-ins that read and change a workspace. Names match
// the registered tools (internal/tool).
var codingToolNames = []string{
	"read_file", "write_file", "patch_file", "list_dir",
	"shell", "git_status", "git_commit",
}

// Research tools: the read-only built-ins, plus any registered tool (MCP,
// skills) whose name says it looks things up.
var researchToolNames = []string{"read_file", "list_dir", "git_status"}

var researchHints = []string{"web", "search", "fetch", "browse", "http", "docs"}

// Resolve computes the ToolExecConfig from the profile. AllowedTools is nil
// when nothing is restricted (every tool the session allows).
func (tp *ToolPolicy) Resolve() ToolExecConfig {
	var allowed []string
	var reasons []string
	research := func() []string {
		out := append([]string{}, researchToolNames...)
		for _, n := range tp.AllAvailableTools {
			l := strings.ToLower(n)
			for _, h := range researchHints {
				if strings.Contains(l, h) {
					out = append(out, n)
					break
				}
			}
		}
		return out
	}

	switch {
	case tp.Profile.HasTool(RestrictedTools):
		// the profile's own list, else the coding tools
		allowed = tp.Profile.AllowedTools
		if len(allowed) == 0 {
			allowed = codingToolNames
		}
		reasons = append(reasons, "restricted: "+itoa(len(allowed))+" tools allowed")

	case tp.Profile.HasTool(CodingTools) && tp.Profile.HasTool(ResearchTools):
		allowed = union(codingToolNames, research())
		reasons = append(reasons, "coding+research tools")

	case tp.Profile.HasTool(CodingTools):
		allowed = codingToolNames
		reasons = append(reasons, "coding tools")

	case tp.Profile.HasTool(ResearchTools):
		allowed = research()
		reasons = append(reasons, "research tools (read-only)")

	default:
		reasons = append(reasons, "all tools (no restriction)")
	}

	// only tools that exist; a policy that leaves nothing falls back to the
	// read-only built-ins rather than a run with no tools at all
	if allowed != nil && len(tp.AllAvailableTools) > 0 {
		allowed = intersect(allowed, tp.AllAvailableTools)
		if len(allowed) == 0 {
			allowed = intersect(researchToolNames, tp.AllAvailableTools)
		}
	}

	concurrency := tp.Profile.EffectiveToolConcurrency()
	if tp.Profile.HasTool(ParallelTools) {
		reasons = append(reasons, "read-only tool calls in parallel (max "+itoa(concurrency)+")")
	} else {
		concurrency = 1
		reasons = append(reasons, "sequential tool calls")
	}

	return ToolExecConfig{
		AllowedTools:   allowed,
		MaxConcurrency: concurrency,
		Explanation:    strings.Join(reasons, "; "),
	}
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	for _, v := range a {
		seen[v] = true
	}
	out := append([]string{}, a...)
	for _, v := range b {
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}

func intersect(allowed, available []string) []string {
	avail := map[string]bool{}
	for _, v := range available {
		avail[v] = true
	}
	out := make([]string, 0, len(allowed))
	for _, v := range allowed {
		if avail[v] {
			out = append(out, v)
		}
	}
	return out
}
