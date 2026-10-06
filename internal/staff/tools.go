package staff

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

const (
	NoteName = "agent_note"
	AskName  = "ask_colleague"
)

// NoteTool lets an Office agent save a line to its own notes.
type NoteTool struct{ E *Engine }

func (NoteTool) Name() string { return NoteName }
func (NoteTool) Description() string {
	return "Save one line to your own notes, which you will see in every later conversation: " +
		"the user's preferences, a decision, where something lives. Not for the task at hand."
}
func (NoteTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"note":{"type":"string","description":"one short line"}},"required":["note"],"additionalProperties":false}`)
}

// Notes touch nothing on disk; the call is still filed under filesystem so
// a rule that stops all writes stops this one too.
func (NoteTool) RequiredPermission() types.PermissionAction { return types.PermFilesystem }

func (t NoteTool) Call(ctx context.Context, tc tool.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tool.Result{Content: "bad arguments: " + err.Error(), IsError: true}, nil
	}
	me := t.E.ProfileOf(ctx, tc.SessionID)
	if me == "" {
		return tool.Result{Content: "only an agent of the Agent space keeps notes", IsError: true}, nil
	}
	if _, err := t.E.AddNote(ctx, me, in.Note); err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, nil
	}
	return tool.Result{Content: "noted"}, nil
}

// AskTool lets an Office agent hand a part of its job to a colleague and
// wait for the colleague's report.
type AskTool struct{ E *Engine }

func (AskTool) Name() string { return AskName }
func (AskTool) Description() string {
	return "Ask a colleague (by name or title, as listed under Your colleagues) to do a part of the job in their field. " +
		"They work in a conversation of their own and cannot see this one, so give them everything the part needs. " +
		"You get their report back; check what matters before you rely on it."
}
func (AskTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"colleague":{"type":"string","description":"their name or title"},` +
		`"task":{"type":"string","description":"what to do, in one or two sentences"},` +
		`"context":{"type":"string","description":"everything they need: paths, constraints, how to check it"}` +
		`},"required":["colleague","task"],"additionalProperties":false}`)
}
func (AskTool) RequiredPermission() types.PermissionAction { return types.PermFilesystem }

func (t AskTool) Call(ctx context.Context, tc tool.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Colleague string `json:"colleague"`
		Task      string `json:"task"`
		Context   string `json:"context"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tool.Result{Content: "bad arguments: " + err.Error(), IsError: true}, nil
	}
	me := t.E.ProfileOf(ctx, tc.SessionID)
	if me == "" {
		return tool.Result{Content: "only an agent of the Agent space has colleagues", IsError: true}, nil
	}
	if t.E.askedByColleague(ctx, tc.SessionID) {
		return tool.Result{Content: "you were asked by a colleague: do this part yourself", IsError: true}, nil
	}
	c, ok := t.E.findColleague(ctx, me, in.Colleague)
	if !ok {
		var names []string
		for _, x := range t.E.colleagues(ctx, me) {
			names = append(names, x.Name)
		}
		return tool.Result{Content: fmt.Sprintf("no colleague called %q (colleagues: %s)", in.Colleague, strings.Join(names, ", ")), IsError: true}, nil
	}
	brief := strings.TrimSpace(in.Task)
	if extra := strings.TrimSpace(in.Context); extra != "" {
		brief += "\n\nContext:\n" + extra
	}
	task, err := t.E.AssignWait(ctx, AssignOpts{ProfileID: c.ID, Brief: brief, WorkspaceID: tc.WorkspaceID, From: me})
	if err != nil {
		return tool.Result{Content: "could not ask " + c.Name + ": " + err.Error(), IsError: true}, nil
	}
	head := fmt.Sprintf("Report from %s (%s)", c.Name, task.Status)
	switch task.Status {
	case StatusDone:
		return tool.Result{Content: head + ":\n" + task.Report}, nil
	case StatusStopped:
		return tool.Result{Content: head + ": stopped before it finished.\n" + task.Report, IsError: true}, nil
	default:
		return tool.Result{Content: head + ": " + task.Error + "\n" + task.Report, IsError: true}, nil
	}
}
