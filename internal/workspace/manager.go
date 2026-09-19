package workspace

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/Kayra-ML/rove/internal/gitwt"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

type Manager struct {
	store *store.Store
	git   *gitwt.Manager
}

func New(s *store.Store, g *gitwt.Manager) *Manager {
	if g == nil {
		g = gitwt.New()
	}
	return &Manager{store: s, git: g}
}

func (m *Manager) Open(ctx context.Context, path, name string) (types.Workspace, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return types.Workspace{}, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return types.Workspace{}, err
	}
	if existing, err := m.store.GetWorkspaceByPath(ctx, abs); err == nil {
		return existing, nil
	}
	now := time.Now().UTC()
	if name == "" {
		name = filepath.Base(abs)
	}
	w := types.Workspace{ID: id.NewID(), Name: name, Path: abs, DefaultBranch: "main", CreatedAt: now, UpdatedAt: now}
	if m.git.IsRepo(abs) {
		if b, err := m.git.CurrentBranch(abs); err == nil {
			w.DefaultBranch = b
		}
	}
	if err := m.store.UpsertWorkspace(ctx, w); err != nil {
		return types.Workspace{}, err
	}
	return w, nil
}

func (m *Manager) Get(ctx context.Context, id types.ID) (types.Workspace, error) {
	return m.store.GetWorkspace(ctx, id)
}

func (m *Manager) List(ctx context.Context) ([]types.Workspace, error) {
	return m.store.ListWorkspaces(ctx)
}

func (m *Manager) Git() *gitwt.Manager { return m.git }

func (m *Manager) Delete(ctx context.Context, id types.ID) error {
	return m.store.DeleteWorkspace(ctx, id)
}
