package session

import (
	"context"
	"time"

	"github.com/aether-dev/aether/internal/eventbus"
	"github.com/aether-dev/aether/internal/id"
	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/types"
)

type Manager struct {
	store *store.Store
	bus   *eventbus.Bus
}

func New(s *store.Store, bus *eventbus.Bus) *Manager {
	return &Manager{store: s, bus: bus}
}

func (m *Manager) Create(ctx context.Context, title string, agentID, ws types.ID) (types.Session, error) {
	now := time.Now().UTC()
	if title == "" {
		title = "Untitled"
	}
	sess := types.Session{ID: id.NewID(), Title: title, AgentID: agentID, WorkspaceID: ws, CreatedAt: now, UpdatedAt: now}
	if err := m.store.UpsertSession(ctx, sess); err != nil {
		return sess, err
	}
	return sess, nil
}

func (m *Manager) Get(ctx context.Context, id types.ID) (types.Session, error) {
	return m.store.GetSession(ctx, id)
}

func (m *Manager) List(ctx context.Context, ws types.ID) ([]types.Session, error) {
	return m.store.ListSessions(ctx, ws)
}

func (m *Manager) Append(ctx context.Context, msg types.Message) (types.Message, error) {
	if msg.ID == "" {
		msg.ID = id.NewID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if err := m.store.InsertMessage(ctx, msg); err != nil {
		return msg, err
	}
	if m.bus != nil {
		m.bus.Publish(types.Event{
			Type:  types.EventMessageDone,
			Topic: "session." + string(msg.SessionID),
			Payload: map[string]any{
				"sessionId": string(msg.SessionID),
				"role":      string(msg.Role),
			},
		})
	}
	return msg, nil
}

func (m *Manager) History(ctx context.Context, sessionID types.ID) ([]types.Message, error) {
	return m.store.ListMessages(ctx, sessionID)
}

func (m *Manager) Rename(ctx context.Context, id types.ID, title string) error {
	sess, err := m.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	sess.Title = title
	sess.UpdatedAt = time.Now().UTC()
	return m.store.UpsertSession(ctx, sess)
}

func (m *Manager) Delete(ctx context.Context, id types.ID) error {
	return m.store.DeleteSession(ctx, id)
}

func (m *Manager) Truncate(ctx context.Context, sessionID types.ID, keep int) error {
	return m.store.TruncateMessages(ctx, sessionID, keep)
}
