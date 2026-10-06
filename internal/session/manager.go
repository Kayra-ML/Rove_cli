package session

import (
	"context"
	"time"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

type Manager struct {
	store *store.Store
	bus   *eventbus.Bus
}

func New(s *store.Store, bus *eventbus.Bus) *Manager {
	return &Manager{store: s, bus: bus}
}

func (m *Manager) Create(ctx context.Context, title string, agentID, ws types.ID) (types.Session, error) {
	return m.CreateIn(ctx, "", title, agentID, ws)
}

// CreateIn creates a chat in a space (types.SpaceOffice or SpaceChat).
func (m *Manager) CreateIn(ctx context.Context, space, title string, agentID, ws types.ID) (types.Session, error) {
	now := time.Now().UTC()
	if title == "" {
		title = "New chat"
	}
	sess := types.Session{ID: id.NewID(), Title: title, AgentID: agentID, WorkspaceID: ws, Space: space, CreatedAt: now, UpdatedAt: now}
	if err := m.store.UpsertSession(ctx, sess); err != nil {
		return sess, err
	}
	return sess, nil
}

// CreateChild opens a member channel under a team session. It shares the
// parent's agent, workspace and space; its persona is set separately.
func (m *Manager) CreateChild(ctx context.Context, parent types.Session, title string) (types.Session, error) {
	return m.CreateChildIn(ctx, parent, parent.Space, title)
}

// CreateChildIn opens a child channel in a given space (Teamwork phases use
// types.SpaceTeamwork, which keeps them out of the Orchestra team).
func (m *Manager) CreateChildIn(ctx context.Context, parent types.Session, space, title string) (types.Session, error) {
	now := time.Now().UTC()
	sess := types.Session{ID: id.NewID(), Title: title, AgentID: parent.AgentID, WorkspaceID: parent.WorkspaceID, ParentID: parent.ID, Space: space, CreatedAt: now, UpdatedAt: now}
	return sess, m.store.UpsertSession(ctx, sess)
}

// Children lists a session's member channels, in join order.
func (m *Manager) Children(ctx context.Context, id types.ID) ([]types.Session, error) {
	return m.store.ListChildSessions(ctx, id)
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
	if err := m.store.UpsertSession(ctx, sess); err != nil {
		return err
	}
	m.Touched(id)
	return nil
}

// Touched tells listeners a session's title or team changed.
func (m *Manager) Touched(id types.ID) {
	if m.bus != nil {
		m.bus.Publish(types.Event{
			Type:    types.EventSessionUpdated,
			Topic:   "session." + string(id),
			Payload: map[string]any{"sessionId": string(id)},
		})
	}
}

func (m *Manager) Delete(ctx context.Context, id types.ID) error {
	return m.store.DeleteSession(ctx, id)
}

func (m *Manager) Truncate(ctx context.Context, sessionID types.ID, keep int) error {
	return m.store.TruncateMessages(ctx, sessionID, keep)
}
