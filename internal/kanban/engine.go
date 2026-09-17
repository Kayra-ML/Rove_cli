package kanban

import (
	"context"
	"fmt"
	"time"

	"github.com/aether-dev/aether/internal/eventbus"
	"github.com/aether-dev/aether/internal/id"
	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/types"
)

type Engine struct {
	store *store.Store
	bus   *eventbus.Bus
}

func New(s *store.Store, bus *eventbus.Bus) *Engine {
	return &Engine{store: s, bus: bus}
}

func (e *Engine) Create(ctx context.Context, c types.Card) (types.Card, error) {
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = id.NewID()
	}
	if c.Column == "" {
		c.Column = types.ColBacklog
	}
	if !types.ValidColumn(c.Column) {
		return c, fmt.Errorf("invalid column %s", c.Column)
	}
	c.CreatedAt, c.UpdatedAt = now, now
	if err := e.store.UpsertCard(ctx, c); err != nil {
		return c, err
	}
	e.emit(types.EventCardUpdated, c)
	return c, nil
}

func (e *Engine) Get(ctx context.Context, id types.ID) (types.Card, error) {
	return e.store.GetCard(ctx, id)
}

func (e *Engine) List(ctx context.Context, ws types.ID) ([]types.Card, error) {
	return e.store.ListCards(ctx, ws)
}

func (e *Engine) Update(ctx context.Context, c types.Card) (types.Card, error) {
	c.UpdatedAt = time.Now().UTC()
	if err := e.store.UpsertCard(ctx, c); err != nil {
		return c, err
	}
	e.emit(types.EventCardUpdated, c)
	return c, nil
}

func (e *Engine) Move(ctx context.Context, id types.ID, col types.KanbanColumn) (types.Card, error) {
	if !types.ValidColumn(col) {
		return types.Card{}, fmt.Errorf("invalid column %s", col)
	}
	c, err := e.store.GetCard(ctx, id)
	if err != nil {
		return c, err
	}
	if err := e.depsSatisfied(ctx, c, col); err != nil {
		return c, err
	}
	c.Column = col
	c.UpdatedAt = time.Now().UTC()
	if col == types.ColDone {
		c.Status = "done"
	}
	if col == types.ColBlocked {
		c.Status = "blocked"
	}
	if col == types.ColRunning {
		c.Status = "running"
	}
	if err := e.store.UpsertCard(ctx, c); err != nil {
		return c, err
	}
	e.emit(types.EventCardMoved, c)
	return c, nil
}

func (e *Engine) depsSatisfied(ctx context.Context, c types.Card, dest types.KanbanColumn) error {
	if dest != types.ColReady && dest != types.ColRunning {
		return nil
	}
	for _, dep := range c.Dependencies {
		d, err := e.store.GetCard(ctx, dep)
		if err != nil {
			return fmt.Errorf("dependency %s: %w", dep, err)
		}
		if d.Column != types.ColDone {
			return fmt.Errorf("dependency %s is %s, not done", dep, d.Column)
		}
	}
	return nil
}

func (e *Engine) AppendLog(ctx context.Context, id types.ID, entry types.LogEntry) error {
	c, err := e.store.GetCard(ctx, id)
	if err != nil {
		return err
	}
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	c.Logs = append(c.Logs, entry)
	c.UpdatedAt = time.Now().UTC()
	if err := e.store.UpsertCard(ctx, c); err != nil {
		return err
	}
	e.emit(types.EventCardUpdated, c)
	return nil
}

func (e *Engine) SetReview(ctx context.Context, id types.ID, state types.ReviewState) (types.Card, error) {
	c, err := e.store.GetCard(ctx, id)
	if err != nil {
		return c, err
	}
	c.ReviewState = state
	if state == types.ReviewApproved {
		c.Column = types.ColDone
		c.Status = "done"
	}
	if state == types.ReviewRejected {
		c.Column = types.ColReady
		c.Status = "rejected"
	}
	c.UpdatedAt = time.Now().UTC()
	if err := e.store.UpsertCard(ctx, c); err != nil {
		return c, err
	}
	e.emit(types.EventCardUpdated, c)
	return c, nil
}

func (e *Engine) Delete(ctx context.Context, id types.ID) error {
	return e.store.DeleteCard(ctx, id)
}

func (e *Engine) Assign(ctx context.Context, id, agentID types.ID) (types.Card, error) {
	c, err := e.store.GetCard(ctx, id)
	if err != nil {
		return c, err
	}
	c.AssigneeAgentID = agentID
	c.UpdatedAt = time.Now().UTC()
	if err := e.store.UpsertCard(ctx, c); err != nil {
		return c, err
	}
	e.emit(types.EventCardUpdated, c)
	return c, nil
}

func (e *Engine) emit(kind types.EventType, c types.Card) {
	if e.bus == nil {
		return
	}
	e.bus.Publish(types.Event{
		Type:  kind,
		Topic: "card." + string(c.ID),
		Payload: map[string]any{
			"id":     string(c.ID),
			"column": string(c.Column),
			"title":  c.Title,
		},
	})
}
