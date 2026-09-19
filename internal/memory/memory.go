package memory

import (
	"context"
	"time"

	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

type System struct {
	store *store.Store
}

func New(s *store.Store) *System { return &System{store: s} }

func (sys *System) Remember(ctx context.Context, scope types.MemoryScope, scopeID types.ID, key, content string) (types.MemoryEntry, error) {
	e := types.MemoryEntry{ID: id.NewID(), Scope: scope, ScopeID: scopeID, Key: key, Content: content, CreatedAt: time.Now().UTC()}
	return e, sys.store.PutMemory(ctx, e)
}

func (sys *System) Recall(ctx context.Context, scope types.MemoryScope, scopeID types.ID, key string) (types.MemoryEntry, error) {
	return sys.store.GetMemory(ctx, scope, scopeID, key)
}

func (sys *System) List(ctx context.Context, scope types.MemoryScope, scopeID types.ID) ([]types.MemoryEntry, error) {
	return sys.store.ListMemory(ctx, scope, scopeID)
}

func (sys *System) PromptBlock(ctx context.Context, agentID, workspaceID, sessionID types.ID) string {
	var out string
	appendScope := func(title string, items []types.MemoryEntry) {
		if len(items) == 0 {
			return
		}
		out += "\n## " + title + "\n"
		for _, it := range items {
			out += "- " + it.Key + ": " + it.Content + "\n"
		}
	}
	if g, err := sys.store.ListMemory(ctx, types.MemGlobal, ""); err == nil {
		appendScope("Global memory", g)
	}
	if workspaceID != "" {
		if w, err := sys.store.ListMemory(ctx, types.MemWorkspace, workspaceID); err == nil {
			appendScope("Workspace memory", w)
		}
	}
	if agentID != "" {
		if a, err := sys.store.ListMemory(ctx, types.MemAgent, agentID); err == nil {
			appendScope("Agent memory", a)
		}
	}
	if sessionID != "" {
		if s, err := sys.store.ListMemory(ctx, types.MemSession, sessionID); err == nil {
			appendScope("Session memory", s)
		}
	}
	return out
}
