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

// Coding tool names (subset of standard tools).
var codingToolNames = []string{
	"fs.read", "fs.write", "fs.patch", "fs.tree", "fs.search",
	"shell.run", "git.status", "git.commit", "git.log",
	"terminal.spawn", "terminal.write",
}

// Research tool names.
var researchToolNames = []string{
	"web.search", "web.fetch", "browser.open", "browser.screenshot",
	"fs.read", "fs.search",
}

// Resolve computes the ToolExecConfig from the profile.
func (tp *ToolPolicy) Resolve() ToolExecConfig {
	var allowed []string
	var reasons []string

	switch {
	case tp.Profile.HasTool(RestrictedTools):
		// Use explicitly configured set, fall back to coding tools.
		allowed = tp.Profile.AllowedTools
		if len(allowed) == 0 {
			allowed = codingToolNames
		}
		reasons = append(reasons, "restricted: "+itoa(len(allowed))+" tools allowed")

	case tp.Profile.HasTool(CodingTools) && tp.Profile.HasTool(ResearchTools):
		allowed = union(codingToolNames, researchToolNames)
		reasons = append(reasons, "coding+research tools")

	case tp.Profile.HasTool(CodingTools):
		allowed = codingToolNames
		reasons = append(reasons, "coding tools")

	case tp.Profile.HasTool(ResearchTools):
		allowed = researchToolNames
		reasons = append(reasons, "research tools")

	default:
		// No restriction — expose all available tools.
		allowed = tp.AllAvailableTools
		reasons = append(reasons, "all tools (no restriction)")
	}

	// Intersect with actually-registered tools.
	if len(tp.AllAvailableTools) > 0 {
		allowed = intersect(allowed, tp.AllAvailableTools)
	}

	concurrency := tp.Profile.EffectiveToolConcurrency()
	if tp.Profile.HasTool(ParallelTools) {
		reasons = append(reasons, "parallel tool calls (max "+itoa(concurrency)+")")
	} else {
		reasons = append(reasons, "sequential tool calls")
	}

	return ToolExecConfig{
		AllowedTools:   allowed,
		MaxConcurrency: concurrency,
		Explanation:    strings.Join(reasons, "; "),
	}
}

// FilterSpecs filters a tool spec list to only allowed names.
func FilterSpecs[T interface{ GetName() string }](specs []T, cfg ToolExecConfig) []T {
	if len(cfg.AllowedTools) == 0 {
		return specs
	}
	allowed := map[string]bool{}
	for _, name := range cfg.AllowedTools {
		allowed[name] = true
	}
	out := make([]T, 0, len(specs))
	for _, s := range specs {
		if allowed[s.GetName()] {
			out = append(out, s)
		}
	}
	return out
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