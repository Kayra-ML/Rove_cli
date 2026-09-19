package permission

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

type Check struct {
	Action types.PermissionAction
	Target string
	Skill  string
	Agent  types.ID
}

type Engine struct {
	store *store.Store
}

func New(s *store.Store) *Engine { return &Engine{store: s} }

func (e *Engine) Evaluate(ctx context.Context, c Check) (types.PermissionDecision, error) {
	rules, err := e.store.ListPermissions(ctx)
	if err != nil {
		return types.PermAsk, err
	}
	var best *types.PermissionRule
	bestScore := -1
	for i := range rules {
		r := rules[i]
		if r.Action != c.Action {
			continue
		}
		if r.Skill != "" && r.Skill != c.Skill {
			continue
		}
		if r.AgentID != "" && r.AgentID != c.Agent {
			continue
		}
		if !match(r.Pattern, c.Target) {
			continue
		}
		score := 0
		if r.Skill != "" {
			score += 2
		}
		if r.AgentID != "" {
			score += 2
		}
		score += len(r.Pattern)
		if score > bestScore {
			bestScore = score
			best = &rules[i]
		}
	}
	if best == nil {
		return types.PermAsk, nil
	}
	return best.Decision, nil
}

func (e *Engine) Allow(ctx context.Context, c Check) error {
	d, err := e.Evaluate(ctx, c)
	if err != nil {
		return err
	}
	if d != types.PermAllow {
		return Denied{Action: c.Action, Target: c.Target, Decision: d}
	}
	return nil
}

func (e *Engine) Put(ctx context.Context, r types.PermissionRule) error {
	if r.ID == "" {
		r.ID = id.NewID()
	}
	return e.store.UpsertPermission(ctx, r)
}

type Denied struct {
	Action   types.PermissionAction
	Target   string
	Decision types.PermissionDecision
}

func (d Denied) Error() string {
	return "permission " + string(d.Decision) + " for " + string(d.Action) + " " + d.Target
}

func match(pattern, target string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		root := strings.TrimSuffix(pattern, "/**")
		return strings.HasPrefix(filepath.ToSlash(target), filepath.ToSlash(root))
	}
	ok, err := filepath.Match(pattern, target)
	if err != nil {
		return pattern == target
	}
	return ok || strings.HasPrefix(target, strings.TrimSuffix(pattern, "*"))
}
