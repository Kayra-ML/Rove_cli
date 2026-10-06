package ctxmap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

// Tools gives agents two small tools: see which sessions they are wired to,
// and send one a short message. Descriptions are terse on purpose — tool
// schemas ride along with every model request.
func Tools(e *Engine) []tool.Tool {
	return []tool.Tool{linkedTool{e}, messageTool{e}}
}

type linkedTool struct{ e *Engine }

func (linkedTool) Name() string        { return "linked_sessions" }
func (linkedTool) Description() string { return "List sessions cabled to this one on the context map." }
func (linkedTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (linkedTool) RequiredPermission() types.PermissionAction { return types.PermFilesystem }
func (t linkedTool) Call(ctx context.Context, tc tool.Context, _ json.RawMessage) (tool.Result, error) {
	if tc.SessionID == "" {
		return tool.Result{Content: "[]"}, nil
	}
	links, err := t.e.st.ListSessionLinks(ctx, tc.SessionID)
	if err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, err
	}
	type row struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Project   string `json:"project,omitempty"`
		Direction string `json:"direction"`
	}
	var out []row
	for _, l := range links {
		other := l.Other(tc.SessionID)
		s, err := t.e.st.GetSession(ctx, other)
		if err != nil {
			continue
		}
		dir := "both"
		switch {
		case l.Flows(tc.SessionID, other) && !l.Flows(other, tc.SessionID):
			dir = "outgoing"
		case !l.Flows(tc.SessionID, other):
			dir = "incoming"
		}
		out = append(out, row{ID: string(other), Title: title(s), Project: t.e.workspacePath(ctx, s.WorkspaceID), Direction: dir})
	}
	b, _ := json.Marshal(out)
	return tool.Result{Content: string(b)}, nil
}

type messageTool struct{ e *Engine }

func (messageTool) Name() string { return "message_session" }
func (messageTool) Description() string {
	return "Send a short message to a cabled session (id or title); it runs there. Use only when that session must act."
}
func (messageTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"session":{"type":"string"},"message":{"type":"string"}},"required":["session","message"],"additionalProperties":false}`)
}
func (messageTool) RequiredPermission() types.PermissionAction { return types.PermFilesystem }
func (t messageTool) Call(ctx context.Context, tc tool.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Session string `json:"session"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, err
	}
	to, err := t.resolve(ctx, tc.SessionID, in.Session)
	if err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, err
	}
	r, err := t.e.Send(ctx, tc.SessionID, to, in.Message, KindAgent)
	if err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, err
	}
	return tool.Result{Content: "queued " + string(r.ID)}, nil
}

func (t messageTool) resolve(ctx context.Context, from types.ID, ref string) (types.ID, error) {
	ref = strings.TrimSpace(ref)
	links, err := t.e.st.ListSessionLinks(ctx, from)
	if err != nil {
		return "", err
	}
	for _, l := range links {
		other := l.Other(from)
		if string(other) == ref {
			return other, nil
		}
		if s, err := t.e.st.GetSession(ctx, other); err == nil && strings.EqualFold(title(s), ref) {
			return other, nil
		}
	}
	return "", fmt.Errorf("no cabled session %q; call linked_sessions", ref)
}
