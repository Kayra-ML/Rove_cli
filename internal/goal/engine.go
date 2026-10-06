package goal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/harness"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/judge"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/internal/workspace"
)

// ErrRunning is returned when a goal is driven while it already runs.
var ErrRunning = errors.New("goal is already running")

// Engine drives autonomous goals using the harness execution kernel.
type Engine struct {
	store       *store.Store
	bus         *eventbus.Bus
	agents      *agent.Runtime
	judge       *judge.Engine
	workspaces  *workspace.Manager
	checkpoints *harness.CheckpointStore
	composer    *harness.HarnessComposer

	// ToolNames lists the registered tools, for the harness tool policy.
	ToolNames func() []string
	// Models lists the configured models, for model fallback.
	Models func(ctx context.Context) []types.ModelRef
	// Snapshot takes a restorable snapshot of a workspace (Sandboxed).
	Snapshot func(dir, label string) error

	mu      sync.Mutex
	running map[types.ID]*run
	base    context.Context
	stop    context.CancelFunc
}

type run struct {
	sessionID types.ID
	cancel    context.CancelFunc
}

func New(s *store.Store, bus *eventbus.Bus, a *agent.Runtime, j *judge.Engine, w *workspace.Manager) *Engine {
	base, stop := context.WithCancel(context.Background())
	return &Engine{
		store:       s,
		bus:         bus,
		agents:      a,
		judge:       j,
		workspaces:  w,
		checkpoints: harness.NewCheckpointStoreWith(checkpointBackend{s}),
		composer:    harness.NewComposer(),
		running:     map[types.ID]*run{},
		base:        base,
		stop:        stop,
	}
}

// checkpointBackend persists harness checkpoints in the store.
type checkpointBackend struct{ st *store.Store }

func (b checkpointBackend) PutCheckpoint(ctx context.Context, goalID, body string) error {
	return b.st.PutGoalCheckpoint(ctx, types.ID(goalID), body)
}
func (b checkpointBackend) GetCheckpoint(ctx context.Context, goalID string) (string, error) {
	return b.st.GetGoalCheckpoint(ctx, types.ID(goalID))
}
func (b checkpointBackend) DeleteCheckpoint(ctx context.Context, goalID string) error {
	return b.st.DeleteGoalCheckpoint(ctx, types.ID(goalID))
}

// Create stores a new goal. A goal made in a chat works in that chat with
// its agent.
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
	if g.AgentID == "" && g.SessionID != "" {
		if s, err := e.store.GetSession(ctx, g.SessionID); err == nil {
			g.AgentID = s.AgentID
			if g.WorkspaceID == "" {
				g.WorkspaceID = s.WorkspaceID
			}
		}
	}
	g.CreatedAt, g.UpdatedAt = now, now
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
	e.Cancel(id)
	return e.store.DeleteGoal(ctx, id)
}

// Start drives a goal in the background, for as long as the engine lives
// or until it is canceled.
func (e *Engine) Start(goalID, sessionID types.ID, workspacePath string) error {
	e.mu.Lock()
	_, busy := e.running[goalID]
	e.mu.Unlock()
	if busy {
		return ErrRunning
	}
	go func() { _, _ = e.Drive(e.base, goalID, sessionID, workspacePath) }()
	return nil
}

// Cancel stops a running goal; it keeps its checkpoint and can be resumed.
func (e *Engine) Cancel(goalID types.ID) bool {
	e.mu.Lock()
	r, ok := e.running[goalID]
	e.mu.Unlock()
	if ok {
		r.cancel()
	}
	return ok
}

// CancelSession stops the goals running in a chat (its /stop).
func (e *Engine) CancelSession(sessionID types.ID) int {
	if sessionID == "" {
		return 0
	}
	e.mu.Lock()
	var hit []*run
	for _, r := range e.running {
		if r.sessionID == sessionID {
			hit = append(hit, r)
		}
	}
	e.mu.Unlock()
	for _, r := range hit {
		r.cancel()
	}
	return len(hit)
}

// Close stops every running goal.
func (e *Engine) Close() { e.stop() }

// HarnessFor returns (or auto-composes) the HarnessProfile for a goal.
// The result is persisted so restarts reuse the same profile.
func (e *Engine) HarnessFor(ctx context.Context, g types.Goal, workDir string) (*harness.HarnessProfile, error) {
	if raw, err := e.store.LoadGoalHarness(ctx, g.ID); err == nil && raw != "" {
		var hp harness.HarnessProfile
		if json.Unmarshal([]byte(raw), &hp) == nil {
			return &hp, nil
		}
	}
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
	hp := &harness.HarnessProfile{
		GoalID:    string(g.ID),
		Current:   e.composer.Compose(analysis),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	raw, _ := json.Marshal(hp)
	_ = e.store.SaveGoalHarness(ctx, g.ID, string(raw))
	return hp, nil
}

// Hand-off budgets: a progress line per iteration, and how many are kept.
const (
	progressLine = 220
	progressKeep = 12
	retryCap     = 2 // retries of a failed agent run within one iteration
)

// Drive runs Goal → harness kernel → agent → gates → reviewer until DONE,
// BLOCKED, the iteration limit, or a cancel. The harness profile governs
// how each iteration runs; the agent's own claim of completion never ends a
// goal — the reviewer and the quality gates do.
func (e *Engine) Drive(ctx context.Context, goalID types.ID, sessionID types.ID, workspacePath string) (types.Goal, error) {
	g, err := e.store.GetGoal(ctx, goalID)
	if err != nil {
		return g, err
	}
	if sessionID == "" {
		sessionID = g.SessionID
	} else if g.SessionID == "" {
		g.SessionID = sessionID
	}
	if g.AgentID == "" && sessionID != "" {
		if s, err := e.store.GetSession(ctx, sessionID); err == nil {
			g.AgentID = s.AgentID
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.mu.Lock()
	if _, busy := e.running[g.ID]; busy {
		e.mu.Unlock()
		return g, ErrRunning
	}
	e.running[g.ID] = &run{sessionID: sessionID, cancel: cancel}
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.running, g.ID)
		e.mu.Unlock()
	}()

	if e.agents != nil && g.AgentID == "" {
		return e.finish(g, types.GoalBlocked, "no agent to work on this goal")
	}

	workDir := workspacePath
	if workDir == "" && e.workspaces != nil && g.WorkspaceID != "" {
		if ws, err2 := e.workspaces.Get(ctx, g.WorkspaceID); err2 == nil {
			workDir = ws.Path
		}
	}
	repoFiles := listRepoFiles(workDir)
	hp, err := e.HarnessFor(ctx, g, workDir)
	if err != nil {
		hp = &harness.HarnessProfile{Current: harness.SmallBugProfile()}
	}
	var tools []string
	if e.ToolNames != nil {
		tools = e.ToolNames()
	}
	kernel := harness.NewKernel(harness.KernelOpts{
		GoalID:        string(g.ID),
		WorkDir:       workDir,
		Title:         g.Title,
		Criteria:      g.CompletionContract.Criteria,
		MaxIterations: g.CompletionContract.MaxIterations,
		Profile:       hp.Current,
		Harness:       hp,
		AllTools:      tools,
		RepoFiles:     repoFiles,
		Keywords:      extractKeywords(g),
	})

	// a resumed goal picks up its progress from the checkpoint
	var progress []string
	resumed := false
	if cp, ok := e.checkpoints.Load(ctx, string(g.ID)); ok && hp.Current.HasRecovery(harness.CheckpointResume) {
		progress, resumed = cp.Progress, g.Iteration > 0
	}
	if hp.Current.HasExec(harness.Sandboxed) && e.Snapshot != nil && workDir != "" && g.Iteration == 0 {
		_ = e.Snapshot(workDir, "goal: "+g.Title)
	}
	base := gitBase(workDir)
	retries := (&harness.RecoveryStrategy{Profile: hp.Current}).Resolve().MaxRetries

	g.Status = types.GoalRunning
	g.UpdatedAt = time.Now().UTC()
	_ = e.store.UpsertGoal(ctx, g)
	e.emit(g)
	e.emitHarness(g, kernel)

	briefed := false
	for {
		if ctx.Err() != nil {
			return e.finish(g, types.GoalCanceled, "stopped")
		}
		if max := g.CompletionContract.MaxIterations; max > 0 && g.Iteration >= max {
			return e.finish(g, types.GoalBlocked, "max iterations reached without satisfying the completion contract")
		}
		g.Iteration++
		itCfg := kernel.ResolveIteration(g.Iteration)
		full := !briefed || itCfg.Fresh
		prompt := e.iterationPrompt(g, itCfg, progress, full, resumed && !briefed)
		briefed = true

		var toolsCalled, filesEdited, errs []string
		summary := ""
		if e.agents != nil {
			req := agent.RunRequest{
				AgentID:         g.AgentID,
				SessionID:       sessionID,
				WorkspaceID:     g.WorkspaceID,
				Workspace:       workDir,
				UserMessage:     prompt,
				SystemExtra:     itCfg.SystemExtra,
				MaxTurns:        itCfg.MaxTurns,
				HistoryTurns:    itCfg.HistoryTurns,
				AllowedTools:    itCfg.ToolCfg.AllowedTools,
				ToolConcurrency: itCfg.ToolCfg.MaxConcurrency,
			}
			if itCfg.UseFallbackModel {
				if m, ok := e.fallbackModel(ctx, g, sessionID, hp.Current); ok {
					req.Provider, req.Model = m.Provider, m.Model
				}
			}
			result, runErr := e.agents.Run(ctx, req)
			// Retry: the same iteration again, told what went wrong
			for n := 0; runErr != nil && ctx.Err() == nil && hp.Current.HasRecovery(harness.Retry) && retries > 0 && n < retryCap; n++ {
				retries--
				req.UserMessage = "The previous attempt failed: " + clip(runErr.Error(), 300) + "\nContinue with the same increment."
				result, runErr = e.agents.Run(ctx, req)
			}
			if ctx.Err() != nil {
				return e.finish(g, types.GoalCanceled, "stopped")
			}
			if runErr != nil {
				errs = append(errs, runErr.Error())
				if kernel.OnFailure() {
					return e.finish(g, types.GoalBlocked, "the agent failed and this goal has no recovery: "+clip(runErr.Error(), 300))
				}
			}
			summary = result.Assistant
			toolsCalled = result.ToolsCalled
			filesEdited = result.FilesEdited
		}

		// evidence: what changed since the goal started, and the checks
		stat, diff, changed := workspaceChanges(workDir, base)
		if !itCfg.VerifyCfg.RequestIndependentReview {
			diff = "" // the reviewer reads the diff only when the profile asks
		}
		var gateResults []types.GateResult
		gatesRun := false
		if itCfg.VerifyCfg.RunQualityGates && len(g.CompletionContract.QualityGates) > 0 {
			gateResults = e.judge.RunGates(ctx, g.CompletionContract.QualityGates, workDir)
			gatesRun = true
		}
		reviewers := 1
		if itCfg.VerifyCfg.RequireDoubleReview {
			reviewers = 2
		}
		verdict := e.judge.Judge(ctx, g, judge.Evidence{
			AgentClaimedDone: looksComplete(summary),
			Summary:          summary,
			CriteriaHits:     map[string]bool{},
			AgentID:          g.AgentID,
			DiffStat:         stat,
			Diff:             diff,
			FilesEdited:      union(filesEdited, changed),
			GateResults:      gateResults,
			GatesRun:         gatesRun,
			Reviewers:        reviewers,
			RequireChanges:   itCfg.VerifyCfg.RequireArtifacts,
		}, workDir)
		if ctx.Err() != nil {
			return e.finish(g, types.GoalCanceled, "stopped")
		}
		var failedGates []string
		for _, gr := range verdict.GateResults {
			if !gr.Passed {
				failedGates = append(failedGates, gr.Name)
			}
		}
		g.LastVerdict = &verdict
		_ = e.store.InsertVerdict(ctx, verdict)
		g.UpdatedAt = time.Now().UTC()

		progress = append(progress, progressOf(g.Iteration, summary, verdict))
		if len(progress) > progressKeep {
			progress = progress[len(progress)-progressKeep:]
		}
		e.checkpoints.Save(ctx, harness.Checkpoint{
			GoalID:      string(g.ID),
			Iteration:   g.Iteration,
			ProfileVer:  kernel.Profile().Current.Version,
			WorkDir:     workDir,
			LastSummary: clip(summary, 1000),
			Progress:    progress,
			SavedAt:     time.Now().UTC(),
		})

		if mutated, _ := kernel.ObserveIteration(g.Iteration, errs, failedGates, filesEdited, toolsCalled, summary); mutated {
			for _, m := range kernel.Mutations() {
				if m.Iteration != g.Iteration {
					continue
				}
				oldJSON, _ := json.Marshal(m.OldProfile)
				newJSON, _ := json.Marshal(m.NewProfile)
				_ = e.store.InsertHarnessMutation(ctx, store.HarnessMutationRow{
					ID:         string(id.NewID()),
					GoalID:     string(g.ID),
					Iteration:  m.Iteration,
					Reason:     m.Reason,
					OldProfile: string(oldJSON),
					NewProfile: string(newJSON),
					CreatedAt:  m.Timestamp,
				})
			}
			hpJSON, _ := json.Marshal(kernel.Profile())
			_ = e.store.SaveGoalHarness(ctx, g.ID, string(hpJSON))
			e.emitHarness(g, kernel)
		}

		switch {
		case verdict.Decision == types.JudgeDONE:
			e.checkpoints.Delete(ctx, string(g.ID))
			e.emitVerdict(verdict)
			return e.finish(g, types.GoalDone, "")
		case verdict.Decision == types.JudgeBLOCKED:
			e.emitVerdict(verdict)
			return e.finish(g, types.GoalBlocked, verdict.Reason)
		case kernel.Escalate():
			e.emitVerdict(verdict)
			return e.finish(g, types.GoalBlocked, "needs your input: still stuck after the harness adapted — "+verdict.Reason)
		default:
			g.Status = types.GoalRunning
			_ = e.store.UpsertGoal(ctx, g)
			e.emit(g)
			e.emitVerdict(verdict)
		}
	}
}

// finish records how a drive ended. It writes with a fresh context: a
// canceled drive must still be able to save that it stopped.
func (e *Engine) finish(g types.Goal, status types.GoalStatus, reason string) (types.Goal, error) {
	ctx := context.Background()
	g.Status = status
	g.UpdatedAt = time.Now().UTC()
	if reason != "" && status != types.GoalDone && (g.LastVerdict == nil || g.LastVerdict.Reason != reason) {
		v := types.JudgeVerdict{ID: id.NewID(), GoalID: g.ID, Iteration: g.Iteration, Reason: reason, CreatedAt: g.UpdatedAt}
		if status == types.GoalBlocked {
			v.Decision = types.JudgeBLOCKED
		} else {
			v.Decision = types.JudgeCONTINUE
		}
		g.LastVerdict = &v
		_ = e.store.InsertVerdict(ctx, v)
	}
	_ = e.store.UpsertGoal(ctx, g)
	e.emit(g)
	switch status {
	case types.GoalDone:
		return g, nil
	case types.GoalCanceled:
		return g, context.Canceled
	default:
		return g, fmt.Errorf("goal %s: %s", status, reason)
	}
}

// fallbackModel picks the model a failing goal switches to: the profile's
// own fallback if configured, else the first configured model that is not
// the one the chat runs on now.
func (e *Engine) fallbackModel(ctx context.Context, g types.Goal, sessionID types.ID, p harness.GoalExecProfile) (types.ModelRef, bool) {
	if e.Models == nil {
		return types.ModelRef{}, false
	}
	models := e.Models(ctx)
	current := ""
	if m, err := e.store.GetSessionModel(ctx, sessionID); err == nil {
		current = m.Model
	} else if ag, err := e.store.GetAgent(ctx, g.AgentID); err == nil {
		current = ag.Model
	}
	for _, want := range p.FallbackModelRefs {
		for _, m := range models {
			if want == m.Model || want == m.Provider+"/"+m.Model {
				return m, true
			}
		}
	}
	for _, m := range models {
		if m.Model != "" && m.Model != current {
			return m, true
		}
	}
	return types.ModelRef{}, false
}

// ── prompts ───────────────────────────────────────────────────────────────────

// iterationPrompt is the user message of one iteration. The first (and any
// fresh-context) iteration carries the whole brief; later ones, whose chat
// history already holds it, carry only what changed — the chat is not sent
// the same goal, file list and plan again every iteration.
func (e *Engine) iterationPrompt(g types.Goal, cfg harness.IterationConfig, progress []string, full, resumed bool) string {
	var b strings.Builder
	if full {
		fmt.Fprintf(&b, "Goal: %s\n", g.Title)
		if d := strings.TrimSpace(g.Description); d != "" {
			b.WriteString(d + "\n")
		}
		b.WriteString("\nCompletion criteria:\n")
		for _, c := range g.CompletionContract.Criteria {
			b.WriteString("- " + c + "\n")
		}
	} else {
		fmt.Fprintf(&b, "Goal: %s\n", g.Title)
	}
	fmt.Fprintf(&b, "\nIteration %d of %d.\n", g.Iteration, g.CompletionContract.MaxIterations)
	if g.LastVerdict != nil && g.LastVerdict.Reason != "" {
		b.WriteString("Reviewer: " + clip(g.LastVerdict.Reason, 600) + "\n")
	}
	if len(progress) > 0 && (full || resumed) {
		keep := progress
		if !cfg.RunCtx.CarryProgress && len(keep) > 2 {
			keep = keep[len(keep)-2:]
		}
		if resumed {
			b.WriteString("\nResuming. Progress so far:\n")
		} else {
			b.WriteString("\nProgress so far:\n")
		}
		for _, p := range keep {
			b.WriteString("- " + p + "\n")
		}
	}
	if cfg.Plan != nil && len(cfg.Plan.Steps) > 0 {
		if cfg.Replanned {
			b.WriteString("\nThe previous approach did not work. Make a new plan before changing code.\n")
		}
		if full || cfg.Replanned {
			b.WriteString("\nPlan:\n")
			for i, s := range cfg.Plan.Steps {
				mark := "  "
				if i == cfg.CurrentStep {
					mark = "→ "
				}
				b.WriteString(mark + s.Description + "\n")
			}
		} else if cfg.CurrentStep < len(cfg.Plan.Steps) {
			b.WriteString("Current step: " + cfg.Plan.Steps[cfg.CurrentStep].Description + "\n")
		}
	}
	if full {
		if cfg.RunCtx.RepoMap != "" {
			b.WriteString("\n" + cfg.RunCtx.RepoMap)
		}
		if len(cfg.RunCtx.SelectiveFiles) > 0 {
			fmt.Fprintf(&b, "\nRelevant files (%d):\n", len(cfg.RunCtx.SelectiveFiles))
			for _, f := range cfg.RunCtx.SelectiveFiles {
				b.WriteString("  " + f + "\n")
			}
		}
	}
	b.WriteString("\nWork on the next increment, then stop calling tools and say briefly what you did.")
	return b.String()
}

// progressOf is one iteration's line in the hand-off.
func progressOf(n int, summary string, v types.JudgeVerdict) string {
	did := strings.TrimSpace(summary)
	if i := strings.IndexByte(did, '\n'); i >= 0 {
		did = did[:i]
	}
	if did == "" {
		did = "(no report)"
	}
	return clip(fmt.Sprintf("%d: %s → %s: %s", n, did, v.Decision, v.Reason), progressLine)
}

func (e *Engine) emit(g types.Goal) {
	if e.bus == nil {
		return
	}
	payload := map[string]any{
		"id":        string(g.ID),
		"status":    string(g.Status),
		"iteration": g.Iteration,
	}
	if g.SessionID != "" {
		payload["sessionId"] = string(g.SessionID)
	}
	e.bus.Publish(types.Event{Type: types.EventGoalUpdated, Topic: "goal." + string(g.ID), Payload: payload})
	if g.SessionID != "" {
		e.bus.Publish(types.Event{Type: types.EventGoalUpdated, Topic: "session." + string(g.SessionID), Payload: payload})
	}
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

// ── task analysis (English and Turkish) ───────────────────────────────────────

func has(text string, words ...string) bool {
	l := lower(text)
	for _, w := range words {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// lower folds case, with Turkish dotted/dotless I folded the way a Turkish
// speaker types them.
func lower(s string) string {
	return strings.ToLower(strings.NewReplacer("İ", "i", "I", "ı").Replace(s))
}

func inferTaskType(g types.Goal) string {
	text := g.Title + " " + g.Description
	switch {
	case has(text, "bug", "fix", "hata", "düzelt", "onar", "çöküyor", "crash"):
		return "bug_fix"
	case has(text, "refactor", "refaktör", "yeniden düzenle", "yeniden yapılandır"):
		return "refactor"
	case has(text, "research", "investigate", "araştır", "incele"):
		return "research"
	case has(text, "test"):
		return "test"
	case has(text, "review", "gözden geçir"):
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
	if has(text, "delete", "drop", "migrate", "migration", "sil", "kaldır", "taşı", "geçiş", "veritabanı şema") {
		return 0.7
	}
	if has(text, "refactor", "rewrite", "refaktör", "yeniden yaz") {
		return 0.5
	}
	return 0.2
}

// looksComplete: the agent says it is done (only noted in the verdict; it
// never ends a goal by itself).
func looksComplete(s string) bool {
	return has(s, "done", "complete", "finished", "bitti", "tamamlandı", "tamamladım", "hazır")
}

func extractKeywords(g types.Goal) []string {
	words := splitWords(lower(g.Title + " " + g.Description))
	seen := map[string]bool{}
	var out []string
	for _, w := range words {
		if len([]rune(w)) > 3 && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

func splitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' || r == ':' || r == ';' || r == '|' || r == '/'
	})
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
				name == "__pycache__" || name == ".cache" || name == ".aether" {
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

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b} {
		for _, v := range list {
			if v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
