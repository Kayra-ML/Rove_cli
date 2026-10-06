package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func (s *Server) handleTeamwork(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	var p struct {
		SessionID types.ID `json:"sessionId"`
		PlanID    types.ID `json:"planId"`
		Prompt    string   `json:"prompt"`
		Phase     string   `json:"phase"`
		Character string   `json:"character"`
		// the planner's model (teamwork.plan); empty: the chat's
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
	}
	w := s.app.Work
	switch req.Method {
	case protocol.MethodWorkPlan:
		if p.Model != "" {
			if err := s.knownModel(ctx, p.Provider, p.Model); err != nil {
				return nil, err
			}
		}
		out, err := w.PlanWith(ctx, p.SessionID, p.Prompt, types.ModelRef{Provider: p.Provider, Model: p.Model})
		if err == nil {
			// a chat that starts with a plan is named after it, as after a first message
			if sess, gerr := s.app.Sess.Get(ctx, p.SessionID); gerr == nil && placeholderTitle(sess.Title) {
				if title := sessionTitleFrom(p.Prompt); title != "" {
					_ = s.app.Sess.Rename(ctx, p.SessionID, title)
				}
			}
		}
		return core.MustJSON(out), err
	case protocol.MethodWorkGet:
		out, err := w.Get(ctx, p.SessionID)
		if errors.Is(err, store.ErrNotFound) {
			return json.RawMessage("null"), nil // no plan yet
		}
		return core.MustJSON(out), err
	case protocol.MethodWorkRemovePhase:
		out, err := w.RemovePhase(ctx, p.PlanID, p.Phase)
		return core.MustJSON(out), err
	case protocol.MethodWorkRemoveAgent:
		out, err := w.RemoveAgent(ctx, p.PlanID, p.Phase, p.Character)
		return core.MustJSON(out), err
	case protocol.MethodWorkApprove:
		out, err := w.Approve(ctx, p.PlanID)
		return core.MustJSON(out), err
	case protocol.MethodWorkRetry:
		out, err := w.Retry(ctx, p.PlanID)
		return core.MustJSON(out), err
	case protocol.MethodWorkCancel:
		out, err := w.Cancel(ctx, p.PlanID)
		return core.MustJSON(out), err
	case protocol.MethodWorkDiscard:
		err := w.Discard(ctx, p.PlanID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	}
	return nil, fmt.Errorf("unknown method %s", req.Method)
}
