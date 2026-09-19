package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/gitwt"
	"github.com/Kayra-ML/rove/internal/goal"
	"github.com/Kayra-ML/rove/internal/harness"
	"github.com/Kayra-ML/rove/internal/kanban"
	"github.com/Kayra-ML/rove/internal/lease"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/internal/workspace"
)

// Orchestrator fans independent Kanban cards out to agents. Cards that share
// files must go through the lease coordinator; worktrees isolate git state.
// Each dispatched card resolves a HarnessProfile — either persisted (manual or
// previously auto-composed) or freshly composed — and injects its policy into
// the agent run.
type Orchestrator struct {
	store      *store.Store
	bus        *eventbus.Bus
	kanban     *kanban.Engine
	agents     *agent.Runtime
	goals      *goal.Engine
	sessions   *session.Manager
	leases     *lease.Coordinator
	git        *gitwt.Manager
	workspaces *workspace.Manager
	composer   *harness.HarnessComposer
	mu         sync.Mutex
	running    map[types.ID]context.CancelFunc
}

func New(s *store.Store, bus *eventbus.Bus, k *kanban.Engine, a *agent.Runtime, g *goal.Engine, sess *session.Manager, l *lease.Coordinator, git *gitwt.Manager, w *workspace.Manager) *Orchestrator {
	return &Orchestrator{
		store: s, bus: bus, kanban: k, agents: a, goals: g, sessions: sess, leases: l, git: git, workspaces: w,
		composer: harness.NewComposer(),
		running:  map[types.ID]context.CancelFunc{},
	}
}

type DispatchOpts struct {
	CardID    types.ID
	SessionID types.ID
}

func (o *Orchestrator) Dispatch(ctx context.Context, opts DispatchOpts) error {
	c, err := o.kanban.Get(ctx, opts.CardID)
	if err != nil {
		return err
	}
	if c.AssigneeAgentID == "" {
		return fmt.Errorf("card %s has no assignee", c.ID)
	}
	wsPath := ""
	if o.workspaces != nil && c.WorkspaceID != "" {
		if ws, err := o.workspaces.Get(ctx, c.WorkspaceID); err == nil {
			wsPath = ws.Path
		}
	}
	workDir := wsPath
	if o.git != nil && wsPath != "" && o.git.IsRepo(wsPath) {
		branch := o.git.BranchForCard(c.ID)
		dest := o.git.WorktreePath(wsPath, c.ID)
		if wt, err := o.git.CreateWorktree(wsPath, dest, branch); err == nil {
			workDir = wt.Path
			c.GitBranch = branch
			c.WorktreePath = dest
			_, _ = o.kanban.Update(ctx, c)
		}
	}

	// Resolve harness profile for card-level dispatch.
	hp := o.resolveCardHarness(ctx, c, workDir)

	if _, err := o.kanban.Move(ctx, c.ID, types.ColRunning); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	o.mu.Lock()
	o.running[c.ID] = cancel
	o.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			o.mu.Lock()
			delete(o.running, c.ID)
			o.mu.Unlock()
		}()
		if c.GoalMode && c.GoalID != "" && o.goals != nil {
			// Goal mode: goal engine owns harness lifecycle.
			_, _ = o.goals.Drive(runCtx, c.GoalID, opts.SessionID, workDir)
			return
		}
		// Card-only mode: inject harness policy into agent run.
		req := agent.RunRequest{
			AgentID:     c.AssigneeAgentID,
			SessionID:   opts.SessionID,
			WorkspaceID: c.WorkspaceID,
			Workspace:   workDir,
			CardID:      c.ID,
			UserMessage: cardPrompt(c),
			SystemExtra: harness.SystemExtraFromProfile(hp.Current),
			MaxTurns:    harness.MaxTurnsFromProfile(hp.Current),
		}
		_, _ = o.agents.Run(runCtx, req)
		latest, err := o.kanban.Get(context.Background(), c.ID)
		if err == nil && latest.Column == types.ColRunning {
			_, _ = o.kanban.Move(context.Background(), c.ID, types.ColReview)
		}
	}()
	return nil
}

// resolveCardHarness loads or composes a harness profile for a standalone card.
func (o *Orchestrator) resolveCardHarness(ctx context.Context, c types.Card, _ string) *harness.HarnessProfile {
	if o.store != nil {
		if raw, err := o.store.LoadCardHarness(ctx, c.ID); err == nil && raw != "" {
			var hp harness.HarnessProfile
			if err2 := harness.UnmarshalProfile(raw, &hp); err2 == nil {
				return &hp
			}
		}
	}
	// Auto-compose.
	analysis := harness.TaskAnalysis{
		TaskType:          inferCardTaskType(c),
		Complexity:        inferCardComplexity(c),
		RiskLevel:         0.2,
		AffectedFiles:     len(c.AcceptanceCriteria) * 2,
		RequestedAutonomy: 0.6,
	}
	profile := o.composer.Compose(analysis)
	now := time.Now().UTC()
	return &harness.HarnessProfile{
		CardID:    string(c.ID),
		Current:   profile,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (o *Orchestrator) Cancel(cardID types.ID) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if c, ok := o.running[cardID]; ok {
		c()
		delete(o.running, cardID)
	}
}

func (o *Orchestrator) Running() []types.ID {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]types.ID, 0, len(o.running))
	for id := range o.running {
		out = append(out, id)
	}
	return out
}

// ActiveCount returns the number of cards currently running in parallel.
func (o *Orchestrator) ActiveCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.running)
}

// ActiveCards returns the card IDs currently running in parallel.
func (o *Orchestrator) ActiveCards() []types.ID {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]types.ID, 0, len(o.running))
	for id := range o.running {
		out = append(out, id)
	}
	return out
}

func cardPrompt(c types.Card) string {
	p := c.Title + "\n" + c.Description + "\n"
	if len(c.AcceptanceCriteria) > 0 {
		p += "Acceptance criteria:\n"
		for _, a := range c.AcceptanceCriteria {
			p += "- " + a + "\n"
		}
	}
	return p
}

func inferCardTaskType(c types.Card) string {
	text := c.Title + " " + c.Description
	switch {
	case containsFold(text, "bug") || containsFold(text, "fix"):
		return "bug_fix"
	case containsFold(text, "refactor"):
		return "refactor"
	case containsFold(text, "test"):
		return "test"
	default:
		return "feature"
	}
}

func inferCardComplexity(c types.Card) float64 {
	n := len(c.AcceptanceCriteria)
	switch {
	case n <= 1:
		return 0.2
	case n <= 3:
		return 0.4
	default:
		return 0.6
	}
}

func containsFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := range sub {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}