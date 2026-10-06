package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func (s *Server) handlePanes(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	var p struct {
		SessionID types.ID        `json:"sessionId"`
		ParentID  types.ID        `json:"parentId"`
		From      types.ID        `json:"from"`
		To        string          `json:"to"`
		Message   string          `json:"message"`
		Layout    json.RawMessage `json:"layout"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
	}
	hub := s.app.Panes
	switch req.Method {
	case protocol.MethodTerminalPanes:
		out, err := hub.Panes(ctx, p.SessionID)
		return core.MustJSON(out), err
	case protocol.MethodTerminalNewPane:
		out, err := hub.NewPane(ctx, p.ParentID)
		return core.MustJSON(out), err
	case protocol.MethodTerminalSendPane:
		to, err := hub.Send(ctx, p.From, p.To, p.Message, false)
		return core.MustJSON(to), err
	case protocol.MethodTerminalLayoutGet:
		body, err := s.app.Store.GetTerminalLayout(ctx, p.SessionID)
		if err != nil {
			return json.RawMessage("null"), nil
		}
		return json.RawMessage(body), nil
	case protocol.MethodTerminalLayoutSet:
		if len(p.Layout) == 0 || !json.Valid(p.Layout) {
			return nil, fmt.Errorf("layout required")
		}
		if _, err := s.app.Store.GetSession(ctx, p.SessionID); err != nil {
			return nil, fmt.Errorf("session not found")
		}
		err := s.app.Store.PutTerminalLayout(ctx, p.SessionID, string(p.Layout))
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	}
	return nil, fmt.Errorf("unknown method %s", req.Method)
}
