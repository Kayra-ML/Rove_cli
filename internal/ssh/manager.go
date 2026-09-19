package ssh

import (
	"context"
	"fmt"

	"github.com/Kayra-ML/rove/internal/terminal"
	"github.com/Kayra-ML/rove/internal/types"
)

type Manager struct {
	term *terminal.Engine
}

func New(t *terminal.Engine) *Manager { return &Manager{term: t} }

func (m *Manager) Open(ctx context.Context, target types.SSHTarget, owner types.ID, ws types.ID) (types.TerminalSession, error) {
	if target.Host == "" {
		return types.TerminalSession{}, fmt.Errorf("ssh: host required")
	}
	title := target.Host
	if target.User != "" {
		title = target.User + "@" + target.Host
	}
	return m.term.Spawn(ctx, terminal.SpawnOpts{
		Kind:        types.TermUser,
		OwnerID:     owner,
		WorkspaceID: ws,
		Title:       title,
		SSH:         &target,
		Persistent:  true,
	})
}
