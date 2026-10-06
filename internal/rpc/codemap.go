package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kayra-ML/rove/internal/codemap"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

type mapParams struct {
	Workspace   string   `json:"workspace"`
	WorkspaceID types.ID `json:"workspaceId"`
	Full        bool     `json:"full"`
	Limit       int      `json:"limit"`
	Dir         string   `json:"dir"`
	Q           string   `json:"q"`
	Kind        string   `json:"kind"`
	Focus       []string `json:"focus"`
	Target      string   `json:"target"`
	Direction   string   `json:"direction"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	Files       []string `json:"files"`
	Depth       int      `json:"depth"`
	SessionID   types.ID `json:"sessionId"`
	Note        string   `json:"note"`
}

func (s *Server) handleMap(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	a := s.app
	if a.Map == nil {
		return nil, fmt.Errorf("codemap unavailable")
	}
	var p mapParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
	}
	root := strings.TrimSpace(p.Workspace)
	if root == "" && p.WorkspaceID != "" {
		ws, err := a.WS.Get(ctx, p.WorkspaceID)
		if err != nil {
			return nil, err
		}
		root = ws.Path
	}
	if root == "" && p.SessionID != "" {
		if sess, err := a.Sess.Get(ctx, p.SessionID); err == nil {
			if ws, err := a.WS.Get(ctx, sess.WorkspaceID); err == nil {
				root = ws.Path
			}
		}
	}

	if req.Method == protocol.MethodMapBuild {
		st, err := a.Map.Rebuild(root, p.Full)
		return core.MustJSON(st), err
	}

	var out any
	err := a.Map.With(root, func(ix *codemap.Index) error {
		switch req.Method {
		case protocol.MethodMapStatus:
			out = ix.Status()
		case protocol.MethodMapGraph:
			out = ix.Graph(p.Limit, p.Dir)
		case protocol.MethodMapQuery:
			out = ix.Query(p.Q, p.Kind, p.Limit, p.Focus)
		case protocol.MethodMapNeighbors:
			res, ok := ix.Neighbors(p.Target, p.Direction, p.Limit)
			if !ok {
				return fmt.Errorf("not in graph: %s", p.Target)
			}
			out = res
		case protocol.MethodMapImpact:
			out = map[string]any{"files": ix.Impact(p.Files, p.Depth)}
		case protocol.MethodMapShare:
			out = ix.Brief(p.Files, 16)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if req.Method == protocol.MethodMapShare {
		return s.shareMap(ctx, p, out.(string))
	}
	return core.MustJSON(out), nil
}

// shareMap hands a slice of the project map to a session as context. It is
// appended, not sent: the target agent sees it on its next turn.
func (s *Server) shareMap(ctx context.Context, p mapParams, brief string) (json.RawMessage, error) {
	if p.SessionID == "" {
		return nil, fmt.Errorf("sessionId required")
	}
	if strings.TrimSpace(brief) == "" {
		return nil, fmt.Errorf("none of the files are in the map")
	}
	var b strings.Builder
	b.WriteString("[kod haritası] Bu dosyalar bağlam olarak paylaşıldı:\n")
	if n := strings.TrimSpace(p.Note); n != "" {
		b.WriteString(n + "\n")
	}
	b.WriteString(brief)
	msg, err := s.app.Sess.Append(ctx, types.Message{SessionID: p.SessionID, Role: types.RoleUser, Content: b.String()})
	return core.MustJSON(msg), err
}
