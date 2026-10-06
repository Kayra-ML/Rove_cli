package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// setChatAgent gives a new chat the Office agent it was started with. A
// chat has one agent: helpers are subagents it hands work to, not a cast.
func setChatAgent(ctx context.Context, a *core.App, sessionID types.ID, profileIDs []types.ID) error {
	if len(profileIDs) > 1 {
		return fmt.Errorf("a chat has one agent; got %d", len(profileIDs))
	}
	if _, err := a.Store.GetAgentProfile(ctx, profileIDs[0]); err != nil {
		return fmt.Errorf("profile %s not found", profileIDs[0])
	}
	return a.Store.PutSessionPersona(ctx, types.SessionPersona{SessionID: sessionID, ProfileID: profileIDs[0], UpdatedAt: time.Now().UTC()})
}

func (s *Server) handleSubagents(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	var p struct {
		SessionID types.ID `json:"sessionId"`
		ID        string   `json:"id"`
		Text      string   `json:"text"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
	}
	sub := s.app.Subagents
	if req.Method == protocol.MethodSubagentList {
		if p.SessionID == "" {
			return nil, fmt.Errorf("sessionId required")
		}
		return core.MustJSON(sub.List(ctx, p.SessionID)), nil
	}
	if p.ID == "" {
		return nil, fmt.Errorf("id required")
	}
	switch req.Method {
	case protocol.MethodSubagentStop:
		t, err := sub.Stop(p.ID)
		return core.MustJSON(t), err
	case protocol.MethodSubagentSteer:
		err := sub.SteerTask(p.ID, p.Text)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodSubagentApply:
		t, err := sub.Apply(p.ID)
		return core.MustJSON(t), err
	case protocol.MethodSubagentDiscard:
		t, err := sub.Discard(p.ID)
		return core.MustJSON(t), err
	}
	return nil, fmt.Errorf("unknown method %s", req.Method)
}
