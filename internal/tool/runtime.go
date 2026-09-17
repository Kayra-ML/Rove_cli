package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/aether-dev/aether/internal/permission"
	"github.com/aether-dev/aether/internal/provider"
	"github.com/aether-dev/aether/internal/types"
)

type Context struct {
	AgentID     types.ID
	WorkspaceID types.ID
	Workspace   string
	CardID      types.ID
	SessionID   types.ID
	Skill       string
}

type Result struct {
	Content string
	IsError bool
}

type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	RequiredPermission() types.PermissionAction
	Call(ctx context.Context, tc Context, args json.RawMessage) (Result, error)
}

type Runtime struct {
	mu    sync.RWMutex
	tools map[string]Tool
	perm  *permission.Engine
}

func New(perm *permission.Engine) *Runtime {
	return &Runtime{tools: map[string]Tool{}, perm: perm}
}

func (r *Runtime) Register(t Tool) {
	r.mu.Lock()
	r.tools[t.Name()] = t
	r.mu.Unlock()
}

func (r *Runtime) Specs() []provider.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]provider.ToolSpec, 0, len(names))
	for _, n := range names {
		t := r.tools[n]
		out = append(out, provider.ToolSpec{Name: t.Name(), Description: t.Description(), Parameters: t.Parameters()})
	}
	return out
}

func (r *Runtime) Call(ctx context.Context, name string, tc Context, args json.RawMessage) (Result, error) {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return Result{Content: "unknown tool: " + name, IsError: true}, fmt.Errorf("unknown tool %s", name)
	}
	if r.perm != nil {
		if err := r.perm.Allow(ctx, permission.Check{Action: t.RequiredPermission(), Target: name, Skill: tc.Skill, Agent: tc.AgentID}); err != nil {
			return Result{Content: err.Error(), IsError: true}, err
		}
	}
	return t.Call(ctx, tc, args)
}
