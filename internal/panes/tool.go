package panes

import (
	"context"
	"encoding/json"

	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

// SendName is the tool a terminal's agent uses to hand work to another.
const SendName = "terminal_send"

// SendTool hands a task to another terminal of the same session. It does
// not wait: the other terminal works on it in its own pane.
type SendTool struct{ Hub *Hub }

func (SendTool) Name() string { return SendName }

func (SendTool) Description() string {
	return "Give another terminal of this session (listed in your instructions as T1, T2…) a task or a question. It works on it in its own terminal; this does not wait for the answer. Use it to split work or to ask a terminal that holds a file you need."
}

func (SendTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"terminal":{"type":"string","description":"T2, 2, or the terminal's role"},"message":{"type":"string","description":"a short, self-contained task or question"}},"required":["terminal","message"]}`)
}

func (SendTool) RequiredPermission() types.PermissionAction { return types.PermFilesystem }

func (t SendTool) Call(ctx context.Context, tc tool.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Terminal string `json:"terminal"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, err
	}
	to, err := t.Hub.Send(ctx, tc.SessionID, in.Terminal, in.Message, true)
	if err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, nil
	}
	return tool.Result{Content: "sent to " + to.name() + "; it is working on it in its own terminal."}, nil
}
