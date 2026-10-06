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
	Profile      GoalExecProfile
	WorkspaceDir string
	RepoFiles    []string // populated by RepositoryMap scan
}

// RunContext is the resolved context spec passed to the agent runtime.
type RunContext struct {
	// TokenBudget is the maximum tokens to use for context assembly.
	TokenBudget int

	// SelectiveFiles is the filtered list of files to include (SelectiveContext).
	SelectiveFiles []string

	// RepoMap is the structural outline of the repository (RepositoryMap).
	RepoMap string

	// FreshHandoff: the iteration sees only its own chat turn and a hand-off
	// of earlier progress (see IterationConfig.HistoryTurns).
	FreshHandoff bool

	// CarryProgress: the hand-off lists every earlier iteration's progress
	// line, not only the last one.
	CarryProgress bool

	// MaxListed caps how many files the prompt lists, from TokenBudget.
	MaxListed int

	// Explanation describes why these context choices were made.
	Explanation string
}

// Build resolves the context tactics into a RunContext.
func (ce *ContextEngine) Build(ctx context.Context, taskKeywords []string) RunContext {
	rc := RunContext{
		TokenBudget: ce.Profile.EffectiveContextBudget(),
	}

	var reasons []string

	// what the prompt may spend on paths: about a fiftieth of the budget,
	// at ~10 tokens a path
	rc.MaxListed = clampInt(rc.TokenBudget/500, 10, 80)

	if ce.Profile.Has(SelectiveContext) {
		rc.SelectiveFiles = selectRelevantFiles(ce.RepoFiles, taskKeywords)
		if len(rc.SelectiveFiles) > rc.MaxListed {
			rc.SelectiveFiles = rc.SelectiveFiles[:rc.MaxListed]
		}
		reasons = append(reasons, "selective: "+itoa(len(rc.SelectiveFiles))+" files filtered by task keywords")
	}

	if ce.Profile.Has(RepositoryMap) {
		rc.RepoMap = buildRepoMap(ce.RepoFiles, rc.MaxListed)
		reasons = append(reasons, "repo map: "+itoa(len(ce.RepoFiles))+" files indexed")
	}

	if ce.Profile.Has(FreshContext) {
		rc.FreshHandoff = true
		reasons = append(reasons, "fresh context: this iteration's turn plus a hand-off")
	}

	if ce.Profile.Has(LargeContext) {
		rc.TokenBudget = ce.Profile.EffectiveContextBudget()
		reasons = append(reasons, "large context: budget="+itoa(rc.TokenBudget)+" tokens")
	}

	if ce.Profile.Has(MemoryHeavy) {
		rc.CarryProgress = true
		reasons = append(reasons, "memory heavy: every iteration's progress carried forward")
	}

	rc.Explanation = strings.Join(reasons, "; ")
	return rc
}

// selectRelevantFiles filters files by keyword relevance.
// Files whose path contains a keyword score higher. Nothing matching means
// nothing is listed: a list of unrelated files only costs tokens.
func selectRelevantFiles(files []string, keywords []string) []string {
	if len(keywords) == 0 {
		return nil
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
	return out
}

// buildRepoMap produces a compact structural outline of the repository.
// It groups files by top-level directory with file counts, at most max lines.
func buildRepoMap(files []string, max int) string {
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
	type kv struct {
		k string
		v int
	}
	var pairs []kv
	for k, v := range dirs {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].k < pairs[j].k
	})
	var sb strings.Builder
	sb.WriteString("Repository map:\n")
	for i, p := range pairs {
		if max > 0 && i >= max {
			sb.WriteString("  … " + itoa(len(pairs)-i) + " more folders\n")
			break
		}
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
