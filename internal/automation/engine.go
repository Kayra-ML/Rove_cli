package automation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/goal"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/kanban"
	"github.com/Kayra-ML/rove/internal/orchestrator"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

type Engine struct {
	store *store.Store
	bus   *eventbus.Bus
	kanban *kanban.Engine
	orch  *orchestrator.Orchestrator
	goals *goal.Engine
	agents agentLister

	mu      sync.Mutex
	cancel  context.CancelFunc
}

type agentLister interface {
	List(ctx context.Context) ([]types.Agent, error)
}

func New(s *store.Store, bus *eventbus.Bus, k *kanban.Engine, o *orchestrator.Orchestrator, g *goal.Engine, a agentLister) *Engine {
	return &Engine{store: s, bus: bus, kanban: k, orch: o, goals: g, agents: a}
}

func (e *Engine) List(ctx context.Context) ([]types.AutomationJob, error) {
	return e.store.ListAutomation(ctx)
}

func (e *Engine) Upsert(ctx context.Context, j types.AutomationJob) (types.AutomationJob, error) {
	now := time.Now().UTC()
	if j.ID == "" {
		j.ID = id.NewID()
		j.CreatedAt = now
	}
	if j.EverySeconds <= 0 {
		j.EverySeconds = 30
	}
	if j.Kind == "" {
		j.Kind = types.AutoSweepReady
	}
	if j.Name == "" {
		j.Name = string(j.Kind)
	}
	j.UpdatedAt = now
	if err := e.store.UpsertAutomation(ctx, j); err != nil {
		return j, err
	}
	return j, nil
}

func (e *Engine) Delete(ctx context.Context, id types.ID) error {
	return e.store.DeleteAutomation(ctx, id)
}

func (e *Engine) Start(parent context.Context) {
	e.mu.Lock()
	if e.cancel != nil {
		e.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	e.cancel = cancel
	e.mu.Unlock()
	go e.loop(ctx)
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

func (e *Engine) loop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	_, _ = e.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = e.Tick(ctx)
		}
	}
}

func (e *Engine) Tick(ctx context.Context) ([]types.AutomationJob, error) {
	jobs, err := e.store.ListAutomation(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]types.AutomationJob, 0, len(jobs))
	for _, j := range jobs {
		if !j.Enabled {
			out = append(out, j)
			continue
		}
		due := j.LastRunAt.IsZero() || now.Sub(j.LastRunAt) >= time.Duration(j.EverySeconds)*time.Second
		if !due {
			out = append(out, j)
			continue
		}
		result, runErr := e.run(ctx, j)
		j.LastRunAt = now
		if runErr != nil {
			j.LastResult = runErr.Error()
		} else {
			j.LastResult = result
		}
		j.UpdatedAt = now
		_ = e.store.UpsertAutomation(ctx, j)
		if e.bus != nil {
			e.bus.Publish(types.Event{
				Type:  types.EventAutomation,
				Topic: "automation",
				Payload: map[string]any{
					"id": string(j.ID), "kind": string(j.Kind), "result": j.LastResult,
				},
				Timestamp: now,
			})
		}
		out = append(out, j)
	}
	return out, nil
}

func (e *Engine) run(ctx context.Context, j types.AutomationJob) (string, error) {
	switch j.Kind {
	case types.AutoAssignIdle:
		return e.assignIdle(ctx)
	case types.AutoDriveGoals:
		return e.driveGoals(ctx)
	default:
		return e.sweepReady(ctx)
	}
}

func (e *Engine) sweepReady(ctx context.Context) (string, error) {
	if e.kanban == nil || e.orch == nil {
		return "noop", nil
	}
	cards, err := e.kanban.List(ctx, "")
	if err != nil {
		return "", err
	}
	n := 0
	for _, c := range cards {
		if c.Column != types.ColReady {
			continue
		}
		if c.AssigneeAgentID == "" {
			if id, err := e.pickIdleAgent(ctx); err == nil && id != "" {
				c, err = e.kanban.Assign(ctx, c.ID, id)
				if err != nil {
					continue
				}
			}
		}
		if c.AssigneeAgentID == "" {
			continue
		}
		if err := e.orch.Dispatch(ctx, orchestrator.DispatchOpts{CardID: c.ID, SessionID: c.SessionID}); err == nil {
			n++
		}
	}
	return fmt.Sprintf("dispatched %d", n), nil
}

func (e *Engine) assignIdle(ctx context.Context) (string, error) {
	if e.kanban == nil {
		return "noop", nil
	}
	cards, err := e.kanban.List(ctx, "")
	if err != nil {
		return "", err
	}
	n := 0
	for _, c := range cards {
		if c.Column != types.ColReady && c.Column != types.ColBacklog {
			continue
		}
		if c.AssigneeAgentID != "" {
			continue
		}
		id, err := e.pickIdleAgent(ctx)
		if err != nil || id == "" {
			continue
		}
		if _, err := e.kanban.Assign(ctx, c.ID, id); err == nil {
			n++
		}
	}
	return fmt.Sprintf("assigned %d", n), nil
}

func (e *Engine) driveGoals(ctx context.Context) (string, error) {
	if e.goals == nil {
		return "noop", nil
	}
	list, err := e.goals.List(ctx)
	if err != nil {
		return "", err
	}
	n := 0
	for _, g := range list {
		if g.Status != types.GoalPending {
			continue
		}
		if _, err := e.goals.Drive(ctx, g.ID, "", ""); err == nil {
			n++
		}
	}
	return fmt.Sprintf("drove %d", n), nil
}

func (e *Engine) pickIdleAgent(ctx context.Context) (types.ID, error) {
	if e.agents == nil {
		return "", fmt.Errorf("no agents")
	}
	list, err := e.agents.List(ctx)
	if err != nil {
		return "", err
	}
	for _, a := range list {
		if a.Status == types.AgentIdle || a.Status == "" {
			return a.ID, nil
		}
	}
	if len(list) > 0 {
		return list[0].ID, nil
	}
	return "", fmt.Errorf("empty roster")
}
