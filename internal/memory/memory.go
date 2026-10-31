package memory

import (
	"context"
	"strings"
	"time"

	"github.com/Burak-ML/rove/internal/id"
	"github.com/Burak-ML/rove/internal/store"
	"github.com/Burak-ML/rove/internal/types"
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

// promptBudget caps what memory adds to every request (bytes). Entries go
// in scope order, narrowest last, so the cut falls on the broadest ones.
const promptBudget = 12000

func (sys *System) PromptBlock(ctx context.Context, agentID, workspaceID, sessionID types.ID) string {
	type scope struct {
		title string
		items []types.MemoryEntry
	}
	var scopes []scope
	add := func(title string, kind types.MemoryScope, id types.ID) {
		if items, err := sys.store.ListMemory(ctx, kind, id); err == nil && len(items) > 0 {
			scopes = append(scopes, scope{title, items})
		}
	}
	add("Global memory", types.MemGlobal, "")
	if workspaceID != "" {
		add("Workspace memory", types.MemWorkspace, workspaceID)
	}
	if agentID != "" {
		add("Agent memory", types.MemAgent, agentID)
	}
	if sessionID != "" {
		add("Session memory", types.MemSession, sessionID)
	}
	// fill from the narrowest scope back, then write in the usual order
	parts := make([]string, len(scopes))
	left := promptBudget
	for i := len(scopes) - 1; i >= 0 && left > 0; i-- {
		var b strings.Builder
		b.WriteString("\n## " + scopes[i].title + "\n")
		for _, it := range scopes[i].items {
			line := "- " + it.Key + ": " + it.Content + "\n"
			if b.Len()+len(line) > left {
				b.WriteString("- …(more entries not shown)\n")
				break
			}
			b.WriteString(line)
		}
		parts[i] = b.String()
		left -= b.Len()
	}
	return strings.Join(parts, "")
}
