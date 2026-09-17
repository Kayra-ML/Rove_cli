package harness

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
)

// ContextEngine applies context tactics to produce a RunContext describing
// what information the agent will receive. This is a value type; call
// Build() to resolve it for a specific workspace.
type ContextEngine struct {
	Profile     GoalExecProfile
	WorkspaceDir string
	RepoFiles   []string // populated by RepositoryMap scan
}

// RunContext is the resolved context spec passed to the agent runtime.
type RunContext struct {
	// TokenBudget is the maximum tokens to use for context assembly.
	TokenBudget int

	// SelectiveFiles is the filtered list of files to include (SelectiveContext).
	SelectiveFiles []string

	// RepoMap is the structural outline of the repository (RepositoryMap).
	RepoMap string

	// FreshHandoff indicates the agent should start a new context window.
	FreshHandoff bool

	// MemoryLoad indicates all available memory should be loaded.
	MemoryLoad bool

	// Explanation describes why these context choices were made.
	Explanation string
}

// Build resolves the context tactics into a RunContext.
func (ce *ContextEngine) Build(ctx context.Context, taskKeywords []string) RunContext {
	rc := RunContext{
		TokenBudget: ce.Profile.EffectiveContextBudget(),
	}

	var reasons []string

	if ce.Profile.Has(SelectiveContext) {
		rc.SelectiveFiles = selectRelevantFiles(ce.RepoFiles, taskKeywords)
		reasons = append(reasons, "selective: "+itoa(len(rc.SelectiveFiles))+" files filtered by task keywords")
	}

	if ce.Profile.Has(RepositoryMap) {
		rc.RepoMap = buildRepoMap(ce.RepoFiles)
		reasons = append(reasons, "repo map: "+itoa(len(ce.RepoFiles))+" files indexed")
	}

	if ce.Profile.Has(FreshContext) {
		rc.FreshHandoff = true
		reasons = append(reasons, "fresh context: new model context window will be started")
	}

	if ce.Profile.Has(LargeContext) {
		rc.TokenBudget = ce.Profile.EffectiveContextBudget()
		reasons = append(reasons, "large context: budget="+itoa(rc.TokenBudget)+" tokens")
	}

	if ce.Profile.Has(MemoryHeavy) {
		rc.MemoryLoad = true
		reasons = append(reasons, "memory heavy: all available agent memory loaded")
	}

	rc.Explanation = strings.Join(reasons, "; ")
	return rc
}

// selectRelevantFiles filters files by keyword relevance.
// Files whose path contains a keyword score higher.
func selectRelevantFiles(files []string, keywords []string) []string {
	if len(keywords) == 0 {
		return files
	}
	type scored struct {
		path  string
		score int
	}
	var ss []scored
	for _, f := range files {
		fl := strings.ToLower(f)
		score := 0
		for _, kw := range keywords {
			if strings.Contains(fl, strings.ToLower(kw)) {
				score++
			}
		}
		if score > 0 {
			ss = append(ss, scored{f, score})
		}
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].score > ss[j].score })
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.path)
	}
	// if nothing matched, fall back to all files up to a cap
	if len(out) == 0 {
		cap := 50
		if len(files) < cap {
			cap = len(files)
		}
		return files[:cap]
	}
	return out
}

// buildRepoMap produces a compact structural outline of the repository.
// It groups files by top-level directory with file counts.
func buildRepoMap(files []string) string {
	dirs := map[string]int{}
	for _, f := range files {
		parts := strings.SplitN(filepath.ToSlash(f), "/", 3)
		key := "."
		if len(parts) >= 2 {
			key = parts[0] + "/" + parts[1]
		} else if len(parts) == 1 {
			key = parts[0]
		}
		dirs[key]++
	}
	type kv struct{ k string; v int }
	var pairs []kv
	for k, v := range dirs {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].k < pairs[j].k
	})
	var sb strings.Builder
	sb.WriteString("Repository map:\n")
	for _, p := range pairs {
		sb.WriteString("  " + p.k + " (" + itoa(p.v) + " files)\n")
	}
	return sb.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}