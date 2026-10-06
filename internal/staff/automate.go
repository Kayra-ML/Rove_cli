package staff

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

// The automation tools let a chat set up automations by being asked to, in
// plain words: an agent watching a chat, a cable between two chats, an agent
// given a task on a timer — and list or remove them. What they make is the
// same as what the Session Map and the Team board make by hand, shows there,
// and can be switched off there.
//
// Only a chat the user talks in gets them: not a subagent's channel, not an
// agent's own task, so automations never set up more automations.

const (
	AutoListName     = "automation_list"
	AutoWatchName    = "automation_watch"
	AutoLinkName     = "automation_link"
	AutoScheduleName = "automation_schedule"
	AutoRemoveName   = "automation_remove"
	AutoMonitorName  = "automation_monitor"
	AutoHandoffName  = "automation_handoff"
)

// AutomationTools are the five tools, ready to register.
func AutomationTools(e *Engine) []tool.Tool {
	return []tool.Tool{autoList{e}, autoWatch{e}, autoLink{e}, autoSchedule{e}, autoMonitor{e}, autoHandoff{e}, autoRemove{e}}
}

// IsAutomationTool reports whether a tool name is one of them.
func IsAutomationTool(name string) bool {
	switch name {
	case AutoListName, AutoWatchName, AutoLinkName, AutoScheduleName, AutoMonitorName, AutoHandoffName, AutoRemoveName:
		return true
	}
	return false
}

// CanAutomate reports whether a session may use the automation tools: a
// chat of the user's own in Code or Agent, not a channel and not a task.
func (e *Engine) CanAutomate(ctx context.Context, sessionID types.ID) bool {
	s, err := e.st.GetSession(ctx, sessionID)
	if err != nil || s.ParentID != "" {
		return false
	}
	switch s.Space {
	case "", types.SpaceChat, types.SpaceOffice:
	default:
		return false
	}
	_, err = e.st.StaffTaskBySession(ctx, sessionID)
	return err != nil
}

const automateGuide = "Use only when the user asks for an automation; say what you set up and how to switch it off (the Session Map or the Team board)."

func refused(msg string) (tool.Result, error) { return tool.Result{Content: msg, IsError: true}, nil }

// ── list ──────────────────────────────────────────────────────────────────

type autoList struct{ e *Engine }

func (autoList) Name() string { return AutoListName }
func (autoList) Description() string {
	return "List the agents of the Agent space, recent chats, and the automations set up: watches (an agent watching a chat), cables between chats, and schedules. Call it before setting one up, to get names right."
}
func (autoList) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (autoList) RequiredPermission() types.PermissionAction { return types.PermFilesystem }

func (t autoList) Call(ctx context.Context, tc tool.Context, _ json.RawMessage) (tool.Result, error) {
	e := t.e
	var b strings.Builder
	b.WriteString("Agents:\n")
	profiles, _ := e.st.ListAgentProfiles(ctx)
	if len(profiles) == 0 {
		b.WriteString("- none yet (they are added in Settings → Agent)\n")
	}
	names := map[types.ID]string{}
	for _, p := range profiles {
		names[p.ID] = p.Name
		line := "- " + p.Name
		if p.Title != "" {
			line += " — " + p.Title
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nThis chat: ")
	if s, err := e.st.GetSession(ctx, tc.SessionID); err == nil {
		b.WriteString(fmt.Sprintf("%q (id %s)\n", s.Title, s.ID))
	}
	b.WriteString("\nRecent chats:\n")
	for _, s := range e.chats(ctx, 20) {
		b.WriteString(fmt.Sprintf("- %q (id %s)\n", s.Title, s.ID))
	}
	title := func(id types.ID) string {
		if s, err := e.st.GetSession(ctx, id); err == nil {
			return s.Title
		}
		return string(id)
	}
	b.WriteString("\nWatches:\n")
	ws, _ := e.Watches(ctx)
	if len(ws) == 0 {
		b.WriteString("- none\n")
	}
	for _, w := range ws {
		b.WriteString(fmt.Sprintf("- [%s] %s watches %q: %s%s\n", w.ID, names[w.ProfileID], title(w.SessionID), orDefault(w.Instruction), onOff(w.Enabled)))
	}
	b.WriteString("\nCables between chats:\n")
	links, _ := e.st.ListAllSessionLinks(ctx)
	if len(links) == 0 {
		b.WriteString("- none\n")
	}
	for _, l := range links {
		b.WriteString(fmt.Sprintf("- [%s] %q ↔ %q (%s)\n", l.ID, title(l.SessionA), title(l.SessionB), l.Direction))
	}
	b.WriteString("\nHandoffs (when one finishes, the next takes it on):\n")
	hs, _ := e.Handoffs(ctx)
	if len(hs) == 0 {
		b.WriteString("- none\n")
	}
	for _, h := range hs {
		b.WriteString(fmt.Sprintf("- [%s] %s → %s: %s%s\n", h.ID, names[h.FromID], names[h.ToID], orDefault(h.Instruction), onOff(h.Enabled)))
	}
	b.WriteString("\nMonitors:\n")
	ms, _ := e.Monitors(ctx)
	if len(ms) == 0 {
		b.WriteString("- none\n")
	}
	for _, m := range ms {
		b.WriteString(fmt.Sprintf("- [%s] %s, every %d min: %q → %s%s\n", m.ID, names[m.ProfileID], m.EveryMinutes, m.Command, m.Instruction, onOff(m.Enabled)))
	}
	b.WriteString("\nSchedules:\n")
	ss, _ := e.Schedules(ctx)
	if len(ss) == 0 {
		b.WriteString("- none\n")
	}
	for _, s := range ss {
		b.WriteString(fmt.Sprintf("- [%s] %s, %s: %s%s\n", s.ID, names[s.ProfileID], s.Describe(), s.Instruction, onOff(s.Enabled)))
	}
	return tool.Result{Content: b.String()}, nil
}

func orDefault(s string) string {
	if s == "" {
		return "(review the changes)"
	}
	return s
}

func onOff(on bool) string {
	if on {
		return ""
	}
	return " (off)"
}

// ── watch ─────────────────────────────────────────────────────────────────

type autoWatch struct{ e *Engine }

func (autoWatch) Name() string { return AutoWatchName }
func (autoWatch) Description() string {
	return "Make an agent watch a chat: whenever a turn there changes files, the agent gets a task with this instruction and the changed files, and reports on the Team board. " + automateGuide
}
func (autoWatch) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"agent":{"type":"string","description":"the agent's name or title"},` +
		`"chat":{"type":"string","description":"the chat to watch, by title or id; empty: this chat"},` +
		`"instruction":{"type":"string","description":"what the agent does each time"},` +
		`"filter":{"type":"string","description":"which files count, e.g. \"*.go, src/*\"; empty: all"},` +
		`"daily_limit":{"type":"integer","description":"at most this many tasks a day (default 10)"}` +
		`},"required":["agent","instruction"],"additionalProperties":false}`)
}
func (autoWatch) RequiredPermission() types.PermissionAction { return types.PermFilesystem }

func (t autoWatch) Call(ctx context.Context, tc tool.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Agent       string `json:"agent"`
		Chat        string `json:"chat"`
		Instruction string `json:"instruction"`
		Filter      string `json:"filter"`
		DailyLimit  int    `json:"daily_limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return refused("bad arguments: " + err.Error())
	}
	e := t.e
	p, ok := e.findAgent(ctx, in.Agent)
	if !ok {
		return refused(fmt.Sprintf("no agent called %q; call %s for the names", in.Agent, AutoListName))
	}
	chat, ok := e.findChat(ctx, tc.SessionID, in.Chat)
	if !ok {
		return refused(fmt.Sprintf("no chat called %q; call %s for the chats", in.Chat, AutoListName))
	}
	w, err := e.SaveWatch(ctx, Watch{SessionID: chat.ID, ProfileID: p.ID})
	if err != nil {
		return refused(err.Error())
	}
	w.Instruction, w.Filter, w.DailyLimit, w.Enabled = in.Instruction, in.Filter, in.DailyLimit, true
	if w, err = e.SaveWatch(ctx, w); err != nil {
		return refused(err.Error())
	}
	e.onMap(ctx, string(chat.ID), "agent:"+string(p.ID))
	filter := "any file"
	if w.Filter != "" {
		filter = w.Filter
	}
	return tool.Result{Content: fmt.Sprintf("Set up [%s]: %s watches %q — on changes to %s: %s. At most %d tasks a day. It shows as a cable on the Session Map.",
		w.ID, p.Name, chat.Title, filter, w.Instruction, w.limit())}, nil
}

// ── link ──────────────────────────────────────────────────────────────────

type autoLink struct{ e *Engine }

func (autoLink) Name() string { return AutoLinkName }
func (autoLink) Description() string {
	return "Cable this chat to another chat (another project, say a website and its app): when one changes files, the other is told what changed and mirrors its counterpart. " + automateGuide
}
func (autoLink) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"chat":{"type":"string","description":"the other chat, by title or id"},` +
		`"direction":{"type":"string","enum":["both","to","from"],"description":"both ways (default), only from this chat to it, or only from it to this chat"},` +
		`"label":{"type":"string","description":"a short name for the cable"}` +
		`},"required":["chat"],"additionalProperties":false}`)
}
func (autoLink) RequiredPermission() types.PermissionAction { return types.PermFilesystem }

func (t autoLink) Call(ctx context.Context, tc tool.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Chat      string `json:"chat"`
		Direction string `json:"direction"`
		Label     string `json:"label"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return refused("bad arguments: " + err.Error())
	}
	e := t.e
	if strings.TrimSpace(in.Chat) == "" {
		return refused("name the other chat")
	}
	other, ok := e.findChat(ctx, tc.SessionID, in.Chat)
	if !ok || other.ID == tc.SessionID {
		return refused(fmt.Sprintf("no other chat called %q; call %s for the chats", in.Chat, AutoListName))
	}
	l := types.SessionLink{SessionA: tc.SessionID, SessionB: other.ID, Label: strings.TrimSpace(in.Label), Direction: types.LinkBoth, Auto: true, Mode: types.LinkSmart}
	switch in.Direction {
	case "to":
		l.Direction = types.LinkAToB
	case "from":
		l.Direction = types.LinkBToA
	}
	if existing, err := e.st.ListSessionLinks(ctx, tc.SessionID); err == nil {
		for _, x := range existing {
			if x.Other(tc.SessionID) == other.ID {
				return tool.Result{Content: fmt.Sprintf("These chats are already cabled [%s].", x.ID)}, nil
			}
		}
	}
	out, err := e.st.CreateSessionLink(ctx, l)
	if err != nil {
		return refused(err.Error())
	}
	e.onMap(ctx, string(tc.SessionID), string(other.ID))
	return tool.Result{Content: fmt.Sprintf("Cabled [%s] this chat with %q (%s). It shows on the Session Map.", out.ID, other.Title, out.Direction)}, nil
}

// ── schedule ──────────────────────────────────────────────────────────────

type autoSchedule struct{ e *Engine }

func (autoSchedule) Name() string { return AutoScheduleName }
func (autoSchedule) Description() string {
	return "Give an agent a task on a timer: every N minutes (at least 15) or every day at a time (\"09:00\", local). Each run reports on the Team board. " + automateGuide
}
func (autoSchedule) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"agent":{"type":"string","description":"the agent's name or title"},` +
		`"instruction":{"type":"string","description":"what the agent does each time"},` +
		`"every_minutes":{"type":"integer","description":"repeat every this many minutes"},` +
		`"daily_at":{"type":"string","description":"or run every day at this local time, HH:MM"}` +
		`},"required":["agent","instruction"],"additionalProperties":false}`)
}
func (autoSchedule) RequiredPermission() types.PermissionAction { return types.PermFilesystem }

func (t autoSchedule) Call(ctx context.Context, tc tool.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Agent        string `json:"agent"`
		Instruction  string `json:"instruction"`
		EveryMinutes int    `json:"every_minutes"`
		DailyAt      string `json:"daily_at"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return refused("bad arguments: " + err.Error())
	}
	e := t.e
	p, ok := e.findAgent(ctx, in.Agent)
	if !ok {
		return refused(fmt.Sprintf("no agent called %q; call %s for the names", in.Agent, AutoListName))
	}
	ws := tc.WorkspaceID
	if s, err := e.st.GetSession(ctx, tc.SessionID); err == nil && ws == "" {
		ws = s.WorkspaceID
	}
	s, err := e.SaveSchedule(ctx, Schedule{ProfileID: p.ID, Instruction: in.Instruction, EveryMinutes: in.EveryMinutes, DailyAt: in.DailyAt, WorkspaceID: ws})
	if err != nil {
		return refused(err.Error())
	}
	return tool.Result{Content: fmt.Sprintf("Scheduled [%s]: %s, %s: %s. It shows on the Team board, where it can be switched off.", s.ID, p.Name, s.Describe(), s.Instruction)}, nil
}

// ── monitor ───────────────────────────────────────────────────────────────

type autoMonitor struct{ e *Engine }

func (autoMonitor) Name() string { return AutoMonitorName }
func (autoMonitor) Description() string {
	return "Watch something outside the project for an agent: a shell command (gh, sentry-cli, curl…) run every N minutes (at least 5) in this project's folder; when its output has lines not seen before, the agent gets a task with them. Nothing new costs no tokens. " +
		"Ready-made commands: GitHub CI failures `" + MonitorPresets[0].Command + "`, new GitHub bugs `" + MonitorPresets[1].Command + "`. " + automateGuide
}
func (autoMonitor) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"agent":{"type":"string","description":"the agent's name or title"},` +
		`"name":{"type":"string","description":"a short name, e.g. CI failures"},` +
		`"command":{"type":"string","description":"the shell command; one item per output line"},` +
		`"instruction":{"type":"string","description":"what the agent does with the new items"},` +
		`"every_minutes":{"type":"integer","description":"check every this many minutes (default 15)"}` +
		`},"required":["agent","command","instruction"],"additionalProperties":false}`)
}

// A monitor runs a shell command on its own from then on: it asks like the
// shell does.
func (autoMonitor) RequiredPermission() types.PermissionAction { return types.PermShell }

func (t autoMonitor) Call(ctx context.Context, tc tool.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Agent        string `json:"agent"`
		Name         string `json:"name"`
		Command      string `json:"command"`
		Instruction  string `json:"instruction"`
		EveryMinutes int    `json:"every_minutes"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return refused("bad arguments: " + err.Error())
	}
	e := t.e
	p, ok := e.findAgent(ctx, in.Agent)
	if !ok {
		return refused(fmt.Sprintf("no agent called %q; call %s for the names", in.Agent, AutoListName))
	}
	if in.EveryMinutes == 0 {
		in.EveryMinutes = 15
	}
	ws := tc.WorkspaceID
	if s, err := e.st.GetSession(ctx, tc.SessionID); err == nil && ws == "" {
		ws = s.WorkspaceID
	}
	m, err := e.SaveMonitor(ctx, Monitor{ProfileID: p.ID, Name: in.Name, Command: in.Command, Instruction: in.Instruction, EveryMinutes: in.EveryMinutes, WorkspaceID: ws})
	if err != nil {
		return refused(err.Error())
	}
	return tool.Result{Content: fmt.Sprintf("Monitor set up [%s]: %s checks %q every %d min; only new lines give it a task (the first check learns what is there already). It shows on the Team board.", m.ID, p.Name, m.Command, m.EveryMinutes)}, nil
}

// ── handoff ───────────────────────────────────────────────────────────────

type autoHandoff struct{ e *Engine }

func (autoHandoff) Name() string { return AutoHandoffName }
func (autoHandoff) Description() string {
	return "Pair two agents: when the first finishes a task, the second gets one — this instruction, the first's report and changed files — and works on top of the first's changes, so the user reviews both together. Chains up to 3 agents; no loops. " + automateGuide
}
func (autoHandoff) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"from":{"type":"string","description":"the agent whose finished work is handed on (name or title)"},` +
		`"to":{"type":"string","description":"the agent who takes it on"},` +
		`"instruction":{"type":"string","description":"what the second does with it, e.g. review and test it"},` +
		`"daily_limit":{"type":"integer","description":"at most this many a day (default 10)"}` +
		`},"required":["from","to"],"additionalProperties":false}`)
}
func (autoHandoff) RequiredPermission() types.PermissionAction { return types.PermFilesystem }

func (t autoHandoff) Call(ctx context.Context, _ tool.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		From        string `json:"from"`
		To          string `json:"to"`
		Instruction string `json:"instruction"`
		DailyLimit  int    `json:"daily_limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return refused("bad arguments: " + err.Error())
	}
	e := t.e
	from, ok := e.findAgent(ctx, in.From)
	if !ok {
		return refused(fmt.Sprintf("no agent called %q; call %s for the names", in.From, AutoListName))
	}
	to, ok := e.findAgent(ctx, in.To)
	if !ok {
		return refused(fmt.Sprintf("no agent called %q; call %s for the names", in.To, AutoListName))
	}
	h, err := e.SaveHandoff(ctx, Handoff{FromID: from.ID, ToID: to.ID})
	if err != nil {
		return refused(err.Error())
	}
	h.Instruction, h.DailyLimit, h.Enabled = in.Instruction, in.DailyLimit, true
	if h, err = e.SaveHandoff(ctx, h); err != nil {
		return refused(err.Error())
	}
	e.onMap(ctx, "agent:"+string(from.ID), "agent:"+string(to.ID))
	return tool.Result{Content: fmt.Sprintf("Paired [%s]: when %s finishes a task, %s takes it on: %s. It shows as a cable between them on the Session Map.", h.ID, from.Name, to.Name, orDefault(h.Instruction))}, nil
}

// ── remove ────────────────────────────────────────────────────────────────

type autoRemove struct{ e *Engine }

func (autoRemove) Name() string { return AutoRemoveName }
func (autoRemove) Description() string {
	return "Remove an automation by the id in brackets that " + AutoListName + " shows: a watch, a cable or a schedule. " + automateGuide
}
func (autoRemove) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`)
}
func (autoRemove) RequiredPermission() types.PermissionAction { return types.PermFilesystem }

func (t autoRemove) Call(ctx context.Context, _ tool.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return refused("bad arguments: " + err.Error())
	}
	e := t.e
	id := types.ID(strings.Trim(strings.TrimSpace(in.ID), "[]"))
	if ws, _ := e.Watches(ctx); hasWatch(ws, id) {
		if err := e.DeleteWatch(ctx, id); err != nil {
			return refused(err.Error())
		}
		e.mapChanged()
		return tool.Result{Content: "Removed the watch."}, nil
	}
	if hs, _ := e.Handoffs(ctx); hasHandoff(hs, id) {
		if err := e.DeleteHandoff(ctx, id); err != nil {
			return refused(err.Error())
		}
		e.mapChanged()
		return tool.Result{Content: "Removed the handoff."}, nil
	}
	if ms, _ := e.Monitors(ctx); hasMonitor(ms, id) {
		if err := e.DeleteMonitor(ctx, id); err != nil {
			return refused(err.Error())
		}
		return tool.Result{Content: "Removed the monitor."}, nil
	}
	if ss, _ := e.Schedules(ctx); hasSchedule(ss, id) {
		if err := e.DeleteSchedule(ctx, id); err != nil {
			return refused(err.Error())
		}
		return tool.Result{Content: "Removed the schedule."}, nil
	}
	if _, err := e.st.GetSessionLink(ctx, id); err == nil {
		if err := e.st.DeleteSessionLink(ctx, id); err != nil {
			return refused(err.Error())
		}
		e.mapChanged()
		return tool.Result{Content: "Removed the cable."}, nil
	}
	return refused(fmt.Sprintf("no automation with id %q; call %s", in.ID, AutoListName))
}

func hasWatch(ws []Watch, id types.ID) bool {
	for _, w := range ws {
		if w.ID == id {
			return true
		}
	}
	return false
}

func hasHandoff(hs []Handoff, id types.ID) bool {
	for _, h := range hs {
		if h.ID == id {
			return true
		}
	}
	return false
}

func hasMonitor(ms []Monitor, id types.ID) bool {
	for _, m := range ms {
		if m.ID == id {
			return true
		}
	}
	return false
}

func hasSchedule(ss []Schedule, id types.ID) bool {
	for _, s := range ss {
		if s.ID == id {
			return true
		}
	}
	return false
}

// ── finding things by name ────────────────────────────────────────────────

func (e *Engine) findAgent(ctx context.Context, q string) (types.AgentProfile, bool) {
	return e.findColleague(ctx, "", q)
}

// chats are the user's own chats in Code and Agent, newest first.
func (e *Engine) chats(ctx context.Context, n int) []types.Session {
	list, err := e.sess.List(ctx, "")
	if err != nil {
		return nil
	}
	var out []types.Session
	for _, s := range list {
		if s.ParentID != "" || (s.Space != "" && s.Space != types.SpaceChat && s.Space != types.SpaceOffice) {
			continue
		}
		out = append(out, s)
		if n > 0 && len(out) >= n {
			break
		}
	}
	return out
}

// findChat finds a chat by id or title (whole title first, then a part of
// it); an empty query is the chat asking.
func (e *Engine) findChat(ctx context.Context, self types.ID, q string) (types.Session, bool) {
	q = strings.TrimSpace(q)
	if q == "" || strings.EqualFold(q, "this chat") || strings.EqualFold(q, "bu sohbet") {
		s, err := e.st.GetSession(ctx, self)
		return s, err == nil
	}
	all := e.chats(ctx, 0)
	for _, s := range all {
		if string(s.ID) == q {
			return s, true
		}
	}
	lq := strings.ToLower(q)
	for _, s := range all {
		if strings.ToLower(s.Title) == lq {
			return s, true
		}
	}
	for _, s := range all {
		if strings.Contains(strings.ToLower(s.Title), lq) {
			return s, true
		}
	}
	return types.Session{}, false
}

// onMap puts cards on the Session Map that are not there yet, to the right
// of what is, so what was just set up can be seen.
func (e *Engine) onMap(ctx context.Context, ids ...string) {
	nodes, err := e.st.ListMapNodes(ctx)
	if err != nil {
		return
	}
	have := map[string]bool{}
	maxX, minY := 0.0, 0.0
	for i, n := range nodes {
		have[string(n.SessionID)] = true
		if n.X > maxX || i == 0 {
			maxX = n.X
		}
		if n.Y < minY || i == 0 {
			minY = n.Y
		}
	}
	x := maxX + 300
	if len(nodes) == 0 {
		x = 0
	}
	y := minY
	for _, id := range ids {
		if have[id] {
			continue
		}
		_ = e.st.PutMapNode(ctx, types.MapNode{SessionID: types.ID(id), X: x, Y: y})
		y += 140
	}
	e.mapChanged()
}

func (e *Engine) mapChanged() {
	if e.bus != nil {
		e.bus.Publish(types.Event{Type: types.EventMapChanged, Topic: "ctxmap", Timestamp: time.Now().UTC()})
	}
}
