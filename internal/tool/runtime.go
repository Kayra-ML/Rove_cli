package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Kayra-ML/rove/internal/permission"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/types"
)

type Context struct {
	AgentID     types.ID
	WorkspaceID types.ID
	Workspace   string
	SessionID   types.ID
	Skill       string
}

type Result struct {
	Content string
	IsError bool
	// Kind is types.ToolRefused when a permission rule stopped the call.
	Kind string
}

type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	RequiredPermission() types.PermissionAction
	Call(ctx context.Context, tc Context, args json.RawMessage) (Result, error)
}

// Approver is asked when a rule neither allows nor denies a call ("ask"):
// it puts the question to the user and reports the answer. Without one, an
// "ask" rule refuses, since nothing can say yes.
type Approver func(ctx context.Context, tc Context, name string, action types.PermissionAction, detail string) (bool, error)

type Runtime struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	perm    *permission.Engine
	approve Approver
}

func New(perm *permission.Engine) *Runtime {
	return &Runtime{tools: map[string]Tool{}, perm: perm}
}

// SetApprover gives the runtime a way to ask the user.
func (r *Runtime) SetApprover(a Approver) {
	r.mu.Lock()
	r.approve = a
	r.mu.Unlock()
}

func (r *Runtime) Register(t Tool) {
	r.mu.Lock()
	r.tools[t.Name()] = t
	r.mu.Unlock()
}

// Unregister drops the tools whose names start with prefix.
func (r *Runtime) Unregister(prefix string) {
	r.mu.Lock()
	for n := range r.tools {
		if strings.HasPrefix(n, prefix) {
			delete(r.tools, n)
		}
	}
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
		check := permission.Check{Action: t.RequiredPermission(), Target: name, Skill: tc.Skill, Agent: tc.AgentID}
		decision, err := r.perm.Evaluate(ctx, check)
		if err != nil {
			return Result{Content: err.Error(), IsError: true}, err
		}
		if decision == types.PermAsk {
			r.mu.RLock()
			ask := r.approve
			r.mu.RUnlock()
			if ask == nil {
				decision = types.PermDeny
			} else {
				ok, askErr := ask(ctx, tc, name, t.RequiredPermission(), Detail(args))
				if askErr != nil {
					return Result{Content: askErr.Error(), IsError: true, Kind: types.ToolRefused}, askErr
				}
				if !ok {
					refused := refusedBy(t.RequiredPermission(), name, "the user said no")
					return Result{Content: refused.Error(), IsError: true, Kind: types.ToolRefused}, refused
				}
				decision = types.PermAllow
			}
		}
		if decision != types.PermAllow {
			denied := permission.Denied{Action: t.RequiredPermission(), Target: name, Decision: decision}
			return Result{Content: denied.Error(), IsError: true, Kind: types.ToolRefused}, denied
		}
	}
	return t.Call(ctx, tc, args)
}

func refusedBy(action types.PermissionAction, name, why string) error {
	return fmt.Errorf("%s was not run: %s. Do not try it again; do the work another way or ask the user to allow %s in Settings → Permissions", name, why, action)
}

// Detail is the part of a call the user needs to see before allowing it:
// the command, the path, the URL — whichever the tool takes.
func Detail(args json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	for _, k := range []string{"command", "path", "url", "query", "message"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	for _, v := range m {
		if sv, ok := v.(string); ok && sv != "" {
			return sv
		}
	}
	return ""
}
