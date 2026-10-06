package automation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/goal"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

type Engine struct {
	store *store.Store
	bus   *eventbus.Bus
	goals *goal.Engine

	mu     sync.Mutex
	cancel context.CancelFunc
}

func New(s *store.Store, bus *eventbus.Bus, g *goal.Engine) *Engine {
	return &Engine{store: s, bus: bus, goals: g}
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
		j.Kind = types.AutoDriveGoals
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

// run does one job. Only goal driving has a runner; a template installed
// for something else does nothing until it gets one.
func (e *Engine) run(ctx context.Context, j types.AutomationJob) (string, error) {
	if j.Kind == types.AutoDriveGoals {
		return e.driveGoals(ctx)
	}
	return "noop", nil
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
