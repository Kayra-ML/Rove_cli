package goal

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/gitwt"
	"github.com/Kayra-ML/rove/internal/harness"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/judge"
	"github.com/Kayra-ML/rove/internal/kanban"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/internal/workspace"
)

// Engine drives autonomous goals using the harness execution kernel.
type Engine struct {
	store      *store.Store
	bus        *eventbus.Bus
	kanban     *kanban.Engine
	agents     *agent.Runtime
	judge      *judge.Engine
	git        *gitwt.Manager
	workspaces *workspace.Manager
	checkpoints *harness.CheckpointStore
	composer   *harness.HarnessComposer
}

func New(s *store.Store, bus *eventbus.Bus, k *kanban.Engine, a *agent.Runtime, j *judge.Engine, g *gitwt.Manager, w *workspace.Manager) *Engine {
	return &Engine{
		store:       s,
		bus:         bus,
		kanban:      k,
		agents:      a,
		judge:       j,
		git:         g,
		workspaces:  w,
		checkpoints: harness.NewCheckpointStore(),
		composer:    harness.NewComposer(),
	}
}

func (e *Engine) Create(ctx context.Context, g types.Goal) (types.Goal, error) {
	now := time.Now().UTC()
	if g.ID == "" {
		g.ID = id.NewID()
	}
	if g.Status == "" {
		g.Status = types.GoalPending
	}
	if g.CompletionContract.MaxIterations == 0 {
		g.CompletionContract.MaxIterations = 8
	}
	g.CreatedAt, g.UpdatedAt = now, now
	if g.CardID == "" && e.kanban != nil {
		card, err := e.kanban.Create(ctx, types.Card{
			Title:              g.Title,
			Description:        g.Description,
			Column:             types.ColReady,
			GoalID:             g.ID,
			GoalMode:           true,
			AcceptanceCriteria: g.CompletionContract.Criteria,
			WorkspaceID:        g.WorkspaceID,
			AssigneeAgentID:    g.AgentID,
		})
		if err != nil {
			return g, err
		}
		g.CardID = card.ID
	}
	if err := e.store.UpsertGoal(ctx, g); err != nil {
		return g, err
	}
	e.emit(g)
	return g, nil
}

func (e *Engine) Get(ctx context.Context, goalID types.ID) (types.Goal, error) {
	return e.store.GetGoal(ctx, goalID)
}

func (e *Engine) List(ctx context.Context) ([]types.Goal, error) {
	return e.store.ListGoals(ctx)
}

func (e *Engine) Delete(ctx context.Context, id types.ID) error {
	return e.store.DeleteGoal(ctx, id)
}

// HarnessFor returns (or auto-composes) the HarnessProfile for a goal.
// The result is persisted so restarts reuse the same profile.
func (e *Engine) HarnessFor(ctx context.Context, g types.Goal, workDir string) (*harness.HarnessProfile, error) {
	// Try to load persisted profile.
	if raw, err := e.store.LoadGoalHarness(ctx, g.ID); err == nil && raw != "" {
		var hp harness.HarnessProfile
		if json.Unmarshal([]byte(raw), &hp) == nil {
			return &hp, nil
		}
	}

	// Compose a new auto profile.
	repoFiles := listRepoFiles(workDir)
	analysis := harness.TaskAnalysis{
		TaskType:          inferTaskType(g),
		Complexity:        inferComplexity(g),
		RiskLevel:         inferRisk(g),
		RepositoryFiles:   len(repoFiles),
		AffectedFiles:     len(g.CompletionContract.Criteria) * 3, // heuristic
		EstimatedMinutes:  float64(g.CompletionContract.MaxIterations) * 5,
		RequestedAutonomy: 0.7,
	}
	profile := e.composer.Compose(analysis)

	hp := &harness.HarnessProfile{
		GoalID:    string(g.ID),
		CardID:    string(g.CardID),
		Current:   profile,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	raw, _ := json.Marshal(hp)
	_ = e.store.SaveGoalHarness(ctx, g.ID, string(raw))

	// Also save to card.
	if g.CardID != "" {
		_ = e.store.SaveCardHarness(ctx, g.CardID, string(raw))
	}
	return hp, nil
}

// Drive runs Goal → harness kernel → agent → gates → judge until DONE/BLOCKED/ctx cancel.
// The harness profile governs how each iteration executes.
// Agent claims of completion are ignored unless the judge agrees.
func (e *Engine) Drive(ctx context.Context, goalID types.ID, sessionID types.ID, workspacePath string) (types.Goal, error) {
	g, err := e.store.GetGoal(ctx, goalID)
	if err != nil {
		return g, err
	}
	workDir := workspacePath
	if workDir == "" && e.workspaces != nil && g.WorkspaceID != "" {
		if ws, err2 := e.workspaces.Get(ctx, g.WorkspaceID); err2 == nil {
			workDir = ws.Path
		}
	}

	// Git worktree isolation.
	if e.git != nil && workDir != "" && e.git.IsRepo(workDir) && g.CardID != "" {
		branch := e.git.BranchForCard(g.CardID)
		dest := e.git.WorktreePath(workDir, g.CardID)
		if wt, wtErr := e.git.CreateWorktree(workDir, dest, branch); wtErr == nil {
			workDir = wt.Path
			if e.kanban != nil {
				if c, err2 := e.kanban.Get(ctx, g.CardID); err2 == nil {
					c.GitBranch = branch
					c.WorktreePath = dest
					_, _ = e.kanban.Update(ctx, c)
				}
			}
		}
	}

	// Resolve or compose harness profile.
	repoFiles := listRepoFiles(workDir)
	hp, err := e.HarnessFor(ctx, g, workDir)
	if err != nil {
		hp = &harness.HarnessProfile{Current: harness.SmallBugProfile()}
	}

	// Build the execution kernel.
	kernelOpts := harness.KernelOpts{
		GoalID:        string(g.ID),
		CardID:        string(g.CardID),
		AgentID:       string(g.AgentID),
		SessionID:     string(sessionID),
		WorkspaceID:   string(g.WorkspaceID),
		WorkDir:       workDir,
		Title:         g.Title,
		Description:   g.Description,
		Criteria:      g.CompletionContract.Criteria,
		MaxIterations: g.CompletionContract.MaxIterations,
		Profile:       hp.Current,
		RepoFiles:     repoFiles,
		Keywords:      extractKeywords(g),
		Checkpoints:   e.checkpoints,
	}
	kernel := harness.NewKernel(kernelOpts)

	g.Status = types.GoalRunning
	g.UpdatedAt = time.Now().UTC()
	_ = e.store.UpsertGoal(ctx, g)
	if e.kanban != nil && g.CardID != "" {
		_, _ = e.kanban.Move(ctx, g.CardID, types.ColRunning)
	}
	e.emit(g)
	e.emitHarness(g, kernel)

	// Save checkpoint for restart recovery.
	e.checkpoints.Save(ctx, harness.Checkpoint{
		GoalID:     string(g.ID),
		CardID:     string(g.CardID),
		Iteration:  g.Iteration,
		ProfileVer: hp.Current.Version,
		WorkDir:    workDir,
		SavedAt:    time.Now().UTC(),
	})

	for {
		if err := ctx.Err(); err != nil {
			return g, err
		}
		g.Iteration++

		// Resolve per-iteration configuration from the kernel.
		itCfg := kernel.ResolveIteration(g.Iteration)

		// Build agent prompt incorporating harness context.
		prompt := e.iterationPrompt(g, itCfg)

		// Run agent.
		var toolsCalled, filesEdited, errs []string
		summary := ""
		if e.agents != nil && g.AgentID != "" {
			result, runErr := e.agents.Run(ctx, agent.RunRequest{
				AgentID:     g.AgentID,
				SessionID:   sessionID,
				WorkspaceID: g.WorkspaceID,
				Workspace:   workDir,
				CardID:      g.CardID,
				UserMessage: prompt,
				SystemExtra: itCfg.SystemExtra,
				MaxTurns:    itCfg.MaxTurns,
			})
			if runErr != nil {
				errs = append(errs, runErr.Error())
				_ = e.kanbanLog(ctx, g, "error", runErr.Error())
			}
			summary = result.Assistant
			toolsCalled = result.ToolsCalled
			filesEdited = result.FilesEdited
		}

		// Determine if agent claimed completion.
		claimed := looksComplete(summary)

		// Run quality gates per verify config.
		var failedGates []string
		var gateResults []types.GateResult
		if itCfg.VerifyCfg.RunQualityGates && len(g.CompletionContract.QualityGates) > 0 {
			gateResults = e.judge.RunGates(ctx, g.CompletionContract.QualityGates, workDir)
			for _, gr := range gateResults {
				if !gr.Passed {
					failedGates = append(failedGates, gr.Name)
				}
			}
		}

		// Judge.
		verdict := e.judge.Judge(ctx, g, judge.Evidence{
			AgentClaimedDone: claimed,
			Summary:          summary,
			CriteriaHits:     map[string]bool{},
		}, workDir)
		if len(gateResults) > 0 {
			verdict.GateResults = gateResults
		}
		g.LastVerdict = &verdict
		_ = e.store.InsertVerdict(ctx, verdict)
		g.UpdatedAt = time.Now().UTC()

		// Persist checkpoint after each iteration.
		e.checkpoints.Save(ctx, harness.Checkpoint{
			GoalID:      string(g.ID),
			CardID:      string(g.CardID),
			Iteration:   g.Iteration,
			ProfileVer:  kernel.Profile().Current.Version,
			WorkDir:     workDir,
			LastSummary: summary,
			SavedAt:     time.Now().UTC(),
		})

		// Observe iteration for stuck detection + possible mutation.
		mutated, mutReason := kernel.ObserveIteration(
			g.Iteration, errs, failedGates, filesEdited, toolsCalled, summary,
		)
		if mutated {
			_ = e.kanbanLog(ctx, g, "warn", "harness mutated: "+mutReason)
			// Persist mutations to store.
			for _, m := range kernel.Mutations() {
				if m.Iteration == g.Iteration {
					oldJSON, _ := json.Marshal(m.OldProfile)
					newJSON, _ := json.Marshal(m.NewProfile)
					_ = e.store.InsertHarnessMutation(ctx, store.HarnessMutationRow{
						ID:         string(id.NewID()),
						GoalID:     string(g.ID),
						CardID:     string(g.CardID),
						Iteration:  m.Iteration,
						Reason:     m.Reason,
						OldProfile: string(oldJSON),
						NewProfile: string(newJSON),
						CreatedAt:  m.Timestamp,
					})
				}
			}
			// Re-persist updated harness.
			hpJSON, _ := json.Marshal(kernel.Profile())
			_ = e.store.SaveGoalHarness(ctx, g.ID, string(hpJSON))
			e.emitHarness(g, kernel)
		}

		switch verdict.Decision {
		case types.JudgeDONE:
			g.Status = types.GoalDone
			_ = e.store.UpsertGoal(ctx, g)
			if e.kanban != nil && g.CardID != "" {
				_, _ = e.kanban.Move(ctx, g.CardID, types.ColReview)
			}
			e.checkpoints.Delete(ctx, string(g.ID))
			e.emit(g)
			e.emitVerdict(verdict)
			return g, nil

		case types.JudgeBLOCKED:
			g.Status = types.GoalBlocked
			_ = e.store.UpsertGoal(ctx, g)
			if e.kanban != nil && g.CardID != "" {
				_, _ = e.kanban.Move(ctx, g.CardID, types.ColBlocked)
			}
			e.emit(g)
			e.emitVerdict(verdict)
			return g, fmt.Errorf("goal blocked: %s", verdict.Reason)

		default:
			g.Status = types.GoalRunning
			_ = e.store.UpsertGoal(ctx, g)
			_ = e.kanbanLog(ctx, g, "info", "CONTINUE: "+verdict.Reason)
			_ = e.kanbanLog(ctx, g, "info", kernel.Explain())
			e.emit(g)
			e.emitVerdict(verdict)
		}
	}
}

// ── Agent result adapter ──────────────────────────────────────────────────────

// RunResult adapter — agent.Runtime.Run returns agent.RunResult; we wrap it
// to extract ToolsCalled and FilesEdited from the RunResult fields.
// (agent.RunResult currently only has Assistant/Turns/Done; we extend here.)

func init() {
	// Verify agent.RunResult has the fields we need at compile time.
	var _ agent.RunResult = agent.RunResult{}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (e *Engine) iterationPrompt(g types.Goal, cfg harness.IterationConfig) string {
	p := fmt.Sprintf("Goal: %s\n%s\n\nCompletion criteria:\n", g.Title, g.Description)
	for _, c := range g.CompletionContract.Criteria {
		p += "- " + c + "\n"
	}
	p += fmt.Sprintf("\nIteration %d of %d.\n", g.Iteration, g.CompletionContract.MaxIterations)
	if g.LastVerdict != nil {
		p += "Previous judge decision: " + string(g.LastVerdict.Decision) + " — " + g.LastVerdict.Reason + "\n"
	}
	if cfg.RunCtx.RepoMap != "" {
		p += "\n" + cfg.RunCtx.RepoMap
	}
	if len(cfg.RunCtx.SelectiveFiles) > 0 {
		p += fmt.Sprintf("\nRelevant files (%d):\n", len(cfg.RunCtx.SelectiveFiles))
		for i, f := range cfg.RunCtx.SelectiveFiles {
			if i >= 20 {
				p += fmt.Sprintf("  … and %d more\n", len(cfg.RunCtx.SelectiveFiles)-20)
				break
			}
			p += "  " + f + "\n"
		}
	}
	p += "\nImplement the next increment. Quality gates will run after you stop calling tools."
	return p
}

func (e *Engine) kanbanLog(ctx context.Context, g types.Goal, level, msg string) error {
	if e.kanban == nil || g.CardID == "" {
		return nil
	}
	return e.kanban.AppendLog(ctx, g.CardID, types.LogEntry{Level: level, Message: msg, Source: "goal"})
}

func (e *Engine) emit(g types.Goal) {
	if e.bus == nil {
		return
	}
	e.bus.Publish(types.Event{
		Type:  types.EventGoalUpdated,
		Topic: "goal." + string(g.ID),
		Payload: map[string]any{
			"id":        string(g.ID),
			"status":    string(g.Status),
			"iteration": g.Iteration,
		},
	})
}

func (e *Engine) emitVerdict(v types.JudgeVerdict) {
	if e.bus == nil {
		return
	}
	e.bus.Publish(types.Event{
		Type:  types.EventJudgeVerdict,
		Topic: "goal." + string(v.GoalID),
		Payload: map[string]any{
			"goalId":   string(v.GoalID),
			"decision": string(v.Decision),
			"reason":   v.Reason,
		},
	})
}

func (e *Engine) emitHarness(g types.Goal, kernel *harness.ExecutionKernel) {
	if e.bus == nil {
		return
	}
	e.bus.Publish(types.Event{
		Type:  "harness.updated",
		Topic: "goal." + string(g.ID),
		Payload: map[string]any{
			"goalId":    string(g.ID),
			"iteration": g.Iteration,
			"explain":   kernel.Explain(),
			"mutations": len(kernel.Mutations()),
		},
	})
}

// ── Utility functions ─────────────────────────────────────────────────────────

func inferTaskType(g types.Goal) string {
	title := g.Title + " " + g.Description
	switch {
	case containsFold(title, "bug") || containsFold(title, "fix"):
		return "bug_fix"
	case containsFold(title, "refactor"):
		return "refactor"
	case containsFold(title, "research") || containsFold(title, "investigate"):
		return "research"
	case containsFold(title, "test"):
		return "test"
	case containsFold(title, "review"):
		return "review"
	default:
		return "feature"
	}
}

func inferComplexity(g types.Goal) float64 {
	n := len(g.CompletionContract.Criteria)
	switch {
	case n <= 1:
		return 0.2
	case n <= 3:
		return 0.4
	case n <= 6:
		return 0.6
	default:
		return 0.8
	}
}

func inferRisk(g types.Goal) float64 {
	text := g.Title + " " + g.Description
	if containsFold(text, "delete") || containsFold(text, "drop") || containsFold(text, "migrate") {
		return 0.7
	}
	if containsFold(text, "refactor") || containsFold(text, "rewrite") {
		return 0.5
	}
	return 0.2
}

func extractKeywords(g types.Goal) []string {
	words := splitWords(g.Title + " " + g.Description)
	seen := map[string]bool{}
	var out []string
	for _, w := range words {
		if len(w) > 3 && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

func splitWords(s string) []string {
	var words []string
	var cur []rune
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' {
			if len(cur) > 0 {
				words = append(words, string(cur))
				cur = cur[:0]
			}
		} else {
			cur = append(cur, r)
		}
	}
	if len(cur) > 0 {
		words = append(words, string(cur))
	}
	return words
}

func listRepoFiles(dir string) []string {
	if dir == "" {
		return nil
	}
	const maxFiles = 2000
	var files []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			name := d.Name()
			// Skip common non-source directories.
			if name == ".git" || name == "vendor" || name == "node_modules" ||
				name == ".next" || name == "dist" || name == "build" ||
				name == "__pycache__" || name == ".cache" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		files = append(files, rel)
		if len(files) >= maxFiles {
			return filepath.SkipAll
		}
		return nil
	})
	return files
}

func lastAssistant(ctx context.Context, e *Engine, sessionID types.ID) string {
	if e.store == nil || sessionID == "" {
		return ""
	}
	msgs, err := e.store.ListMessages(ctx, sessionID)
	if err != nil {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleAssistant {
			return msgs[i].Content
		}
	}
	return ""
}

func looksComplete(s string) bool {
	return containsFold(s, "done") || containsFold(s, "complete")
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && indexFold(s, sub) >= 0))
}

func indexFold(s, sub string) int {
	ls, lsub := []rune(s), []rune(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		ok := true
		for j := range lsub {
			a, b := ls[i+j], lsub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}