package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/harness"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// harnessView is a goal's harness as the app shows it: the profile, what it
// resolves to, where the goal stands (its checkpoint) and how the profile
// changed while it ran.
type harnessView struct {
	GoalID     types.ID                `json:"goalId"`
	Profile    harness.GoalExecProfile `json:"profile"`
	Explain    string                  `json:"explain"`
	Resolved   harness.Resolved        `json:"resolved"`
	Running    bool                    `json:"running"`
	Checkpoint *harness.Checkpoint     `json:"checkpoint,omitempty"`
	Mutations  []mutationView          `json:"mutations"`
}

// mutationView is one change of a goal's profile: the harness's own (when
// it found the goal stuck), or the user's.
type mutationView struct {
	Iteration int                     `json:"iteration"`
	Reason    string                  `json:"reason"`
	Old       harness.GoalExecProfile `json:"old"`
	New       harness.GoalExecProfile `json:"new"`
	Result    string                  `json:"result,omitempty"`
	At        time.Time               `json:"at"`
}

func (s *Server) handleHarness(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	if req.Method == protocol.MethodHarnessCatalog {
		return core.MustJSON(harness.Catalog()), nil
	}
	var p struct {
		GoalID  types.ID                 `json:"goalId"`
		ID      types.ID                 `json:"id"`
		Profile *harness.GoalExecProfile `json:"profile"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
	}
	if p.GoalID == "" {
		p.GoalID = p.ID
	}
	if p.GoalID == "" {
		return nil, errors.New("goalId is required")
	}
	goals := s.app.Goals
	switch req.Method {
	case protocol.MethodGoalCancel:
		return core.MustJSON(map[string]any{"stopped": goals.Cancel(p.GoalID)}), nil

	case protocol.MethodGoalVerdicts:
		if _, err := goals.Get(ctx, p.GoalID); err != nil {
			return nil, err
		}
		out, err := s.app.Store.ListVerdicts(ctx, p.GoalID)
		if out == nil {
			out = []types.JudgeVerdict{}
		}
		return core.MustJSON(out), err

	case protocol.MethodHarnessMutations:
		out, err := s.mutations(ctx, p.GoalID)
		return core.MustJSON(out), err

	case protocol.MethodHarnessGet:
		hp, err := goals.Harness(ctx, p.GoalID)
		if err != nil {
			return nil, err
		}
		v, err := s.harnessView(ctx, p.GoalID, hp)
		return core.MustJSON(v), err

	case protocol.MethodHarnessSet, protocol.MethodHarnessCompose:
		var hp *harness.HarnessProfile
		var err error
		if req.Method == protocol.MethodHarnessSet {
			if p.Profile == nil {
				return nil, errors.New("profile is required")
			}
			hp, err = goals.SetHarness(ctx, p.GoalID, *p.Profile)
		} else {
			hp, err = goals.ComposeHarness(ctx, p.GoalID)
		}
		if err != nil {
			return nil, err
		}
		v, err := s.harnessView(ctx, p.GoalID, hp)
		return core.MustJSON(v), err
	}
	return nil, errors.New("unknown harness method " + req.Method)
}

func (s *Server) harnessView(ctx context.Context, goalID types.ID, hp *harness.HarnessProfile) (harnessView, error) {
	v := harnessView{
		GoalID:   goalID,
		Profile:  hp.Current,
		Explain:  hp.Current.Explain(),
		Resolved: harness.Resolve(hp.Current, s.toolNames()),
		Running:  s.app.Goals.Running(goalID),
	}
	if body, err := s.app.Store.GetGoalCheckpoint(ctx, goalID); err == nil {
		var cp harness.Checkpoint
		if json.Unmarshal([]byte(body), &cp) == nil {
			v.Checkpoint = &cp
		}
	}
	m, err := s.mutations(ctx, goalID)
	v.Mutations = m
	return v, err
}

func (s *Server) mutations(ctx context.Context, goalID types.ID) ([]mutationView, error) {
	rows, err := s.app.Store.ListHarnessMutations(ctx, goalID)
	out := make([]mutationView, 0, len(rows))
	for _, r := range rows {
		m := mutationView{Iteration: r.Iteration, Reason: r.Reason, Result: r.Result, At: r.CreatedAt}
		_ = json.Unmarshal([]byte(r.OldProfile), &m.Old)
		_ = json.Unmarshal([]byte(r.NewProfile), &m.New)
		out = append(out, m)
	}
	return out, err
}

func (s *Server) toolNames() []string {
	if s.app.Tools == nil {
		return nil
	}
	specs := s.app.Tools.Specs()
	out := make([]string, 0, len(specs))
	for _, sp := range specs {
		out = append(out, sp.Name)
	}
	return out
}
