package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/staff"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func (s *Server) handleStaff(ctx context.Context, req protocol.Request) (json.RawMessage, error) {
	var p struct {
		ID          types.ID `json:"id"`
		ProfileID   types.ID `json:"profileId"`
		WorkspaceID types.ID `json:"workspaceId"`
		Task        string   `json:"task"`
		Note        string   `json:"note"`
		Limit       int      `json:"limit"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
	}
	e := s.app.Staff
	switch req.Method {
	case protocol.MethodStaffMembers:
		out, err := e.Members(ctx)
		return core.MustJSON(out), err
	case protocol.MethodStaffAssign:
		if p.ProfileID == "" {
			return nil, fmt.Errorf("profileId required")
		}
		out, err := e.Assign(ctx, staff.AssignOpts{ProfileID: p.ProfileID, Brief: p.Task, WorkspaceID: p.WorkspaceID})
		return core.MustJSON(out), err
	case protocol.MethodStaffTasks:
		out, err := e.Tasks(ctx, p.ProfileID, p.Limit)
		return core.MustJSON(out), err
	case protocol.MethodStaffSeen:
		out, err := e.Seen(ctx, p.ID)
		return core.MustJSON(out), err
	case protocol.MethodStaffStop:
		out, err := e.Stop(ctx, p.ID)
		return core.MustJSON(out), err
	case protocol.MethodStaffNoteAdd:
		if p.ProfileID == "" {
			return nil, fmt.Errorf("profileId required")
		}
		out, err := e.AddNote(ctx, p.ProfileID, p.Note)
		return core.MustJSON(out), err
	case protocol.MethodStaffNoteDelete:
		err := e.DeleteNote(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodStaffWatches:
		out, err := e.Watches(ctx)
		return core.MustJSON(out), err
	case protocol.MethodStaffWatchSave:
		var w staff.Watch
		if err := json.Unmarshal(req.Params, &w); err != nil {
			return nil, err
		}
		out, err := e.SaveWatch(ctx, w)
		if err == nil {
			s.mapChanged()
		}
		return core.MustJSON(out), err
	case protocol.MethodStaffDiff:
		out, err := e.Diff(ctx, p.ID)
		return core.MustJSON(map[string]any{"patch": out}), err
	case protocol.MethodStaffApply:
		out, err := e.Apply(ctx, p.ID)
		return core.MustJSON(out), err
	case protocol.MethodStaffDiscard:
		out, err := e.Discard(ctx, p.ID)
		return core.MustJSON(out), err
	case protocol.MethodStaffPR:
		out, err := e.OpenPR(ctx, p.ID)
		return core.MustJSON(out), err
	case protocol.MethodStaffHandoffs:
		out, err := e.Handoffs(ctx)
		return core.MustJSON(out), err
	case protocol.MethodStaffHandoffSave:
		var h staff.Handoff
		if err := json.Unmarshal(req.Params, &h); err != nil {
			return nil, err
		}
		out, err := e.SaveHandoff(ctx, h)
		if err == nil {
			s.mapChanged()
		}
		return core.MustJSON(out), err
	case protocol.MethodStaffHandoffDelete:
		err := e.DeleteHandoff(ctx, p.ID)
		if err == nil {
			s.mapChanged()
		}
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodStaffMonitors:
		out, err := e.Monitors(ctx)
		return core.MustJSON(out), err
	case protocol.MethodStaffMonitorSave:
		var m staff.Monitor
		if err := json.Unmarshal(req.Params, &m); err != nil {
			return nil, err
		}
		out, err := e.SaveMonitor(ctx, m)
		return core.MustJSON(out), err
	case protocol.MethodStaffMonitorDelete:
		err := e.DeleteMonitor(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodStaffMonitorPresets:
		return core.MustJSON(staff.MonitorPresets), nil
	case protocol.MethodStaffSchedules:
		out, err := e.Schedules(ctx)
		return core.MustJSON(out), err
	case protocol.MethodStaffScheduleSave:
		var sc staff.Schedule
		if err := json.Unmarshal(req.Params, &sc); err != nil {
			return nil, err
		}
		out, err := e.SaveSchedule(ctx, sc)
		return core.MustJSON(out), err
	case protocol.MethodStaffScheduleDelete:
		err := e.DeleteSchedule(ctx, p.ID)
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	case protocol.MethodStaffWatchDelete:
		err := e.DeleteWatch(ctx, p.ID)
		if err == nil {
			s.mapChanged()
		}
		return core.MustJSON(map[string]any{"ok": err == nil}), err
	}
	return nil, fmt.Errorf("unknown method %s", req.Method)
}
