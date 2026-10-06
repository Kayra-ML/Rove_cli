// Package staff makes the Agent space's agents work like a team of people:
// each has a title and notes of its own that it carries into every
// conversation, the user can hand one a task without opening a chat and
// get a report back when it is done, the space shows who is working on
// what, and an agent can ask a colleague to take a part of its job.
//
// An agent here is an Office profile (types.AgentProfile). A task runs in a
// conversation of its own in the Agent space, so the user can open it and
// see — or continue — the work.
package staff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/gitwt"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

// EventUpdated fires on topic "staff" whenever a task changes; its payload
// is the task.
const EventUpdated types.EventType = "staff.updated"

type Status string

const (
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	StatusStopped Status = "stopped"
)

// Task is a piece of work handed to an agent: by the user, or by a
// colleague (From) that waits for its report.
type Task struct {
	ID        types.ID `json:"id"`
	ProfileID types.ID `json:"profileId"`
	// ProfileName is the agent's name when the task was given.
	ProfileName string   `json:"profileName,omitempty"`
	SessionID   types.ID `json:"sessionId"`
	Title       string   `json:"title"`
	Brief       string   `json:"brief"`
	// From is the colleague who asked; empty when the user did.
	From types.ID `json:"from,omitempty"`
	// WatchID is the Session Map watch that set this task off, and Origin
	// the chat it watched; both empty for a task given by hand.
	WatchID    types.ID `json:"watchId,omitempty"`
	ScheduleID types.ID `json:"scheduleId,omitempty"`
	MonitorID  types.ID `json:"monitorId,omitempty"`
	// HandoffID: a colleague's finished task was handed on; CarryFrom is
	// that task (its changes are where this one starts), Chain the agents
	// the work has been through.
	HandoffID   types.ID   `json:"handoffId,omitempty"`
	CarryFrom   types.ID   `json:"carryFrom,omitempty"`
	Chain       []types.ID `json:"chain,omitempty"`
	WorkspaceID types.ID   `json:"workspaceId,omitempty"`
	Origin      string     `json:"origin,omitempty"`
	FromName    string     `json:"fromName,omitempty"`
	Status      Status     `json:"status"`
	Report      string     `json:"report,omitempty"`
	Error       string     `json:"error,omitempty"`
	Seen        bool       `json:"seen"`
	CreatedAt   time.Time  `json:"createdAt"`
	EndedAt     time.Time  `json:"endedAt,omitempty"`
	// Isolation is the task's own checkout, when the project is a git
	// repository, and what became of its work.
	Isolation *Isolation `json:"isolation,omitempty"`
}

func (t Task) done() bool { return t.Status != StatusRunning }

// Member is one agent as the Agent space shows it.
type Member struct {
	Profile types.AgentProfile `json:"profile"`
	// State: working (a task or a chat of its is running), report (a task
	// the user gave it finished and its report is unread) or idle.
	State string `json:"state"`
	// Now is what it is working on, and where.
	Now        string              `json:"now,omitempty"`
	NowSession types.ID            `json:"nowSession,omitempty"`
	Tasks      []Task              `json:"tasks"`
	Notes      []types.MemoryEntry `json:"notes"`
}

// Runner starts one agent run; *agent.Runtime.Run fits.
type Runner func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error)

// Budgets: a task is real work and gets room to read, change and check;
// a report is what the user (or a colleague) reads, not the whole turn.
const (
	taskTurns   = 40
	reportClip  = 1500
	titleRunes  = 60
	recentTasks = 5
)

type Engine struct {
	st   *store.Store
	sess *session.Manager
	bus  *eventbus.Bus

	Run          Runner
	Running      func(types.ID) bool
	Cancel       func(types.ID)
	DefaultAgent func(ctx context.Context) (types.ID, error)
	// WorkspacePath finds a workspace's folder.
	WorkspacePath func(ctx context.Context, id types.ID) string
	// Git and WorktreeRoot give each task a checkout of its own; with
	// either unset, tasks work in the project folder.
	// RunCommand runs a monitor's command; nil uses the shell. Tests set it.
	RunCommand func(ctx context.Context, command, dir string) (string, error)
	// Ask is one tool-less call: the look back after a task.
	Ask          func(ctx context.Context, agentID types.ID, system, user string) (string, error)
	Git          *gitwt.Manager
	WorktreeRoot string
	applyMu      sync.Mutex

	base context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup
}

func New(st *store.Store, sm *session.Manager, bus *eventbus.Bus) *Engine {
	base, stop := context.WithCancel(context.Background())
	return &Engine{st: st, sess: sm, bus: bus, base: base, stop: stop}
}

// Close stops every task in flight and waits for them to record it.
func (e *Engine) Close() {
	e.stop()
	e.wg.Wait()
}

// AssignOpts is a task to hand out.
type AssignOpts struct {
	ProfileID   types.ID
	Brief       string
	WorkspaceID types.ID
	// From is the colleague asking; empty for the user.
	From types.ID
	// AutoApply lands the task's changes as soon as it is done, instead of
	// keeping them for the user: a colleague waiting on it needs them.
	AutoApply bool
	// WatchID or ScheduleID, and Origin: a watch on the Session Map or a
	// timer set it off.
	WatchID    types.ID
	ScheduleID types.ID
	MonitorID  types.ID
	Origin     string
	// HandoffID, CarryFrom, Chain: a paired agent's finished task handed on.
	HandoffID types.ID
	CarryFrom types.ID
	Chain     []types.ID
}

// Assign hands an agent a task and returns at once; the agent works in the
// background in a conversation of its own and reports when done.
func (e *Engine) Assign(ctx context.Context, o AssignOpts) (Task, error) {
	t, ws, err := e.open(ctx, o)
	if err != nil {
		return t, err
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.play(e.base, t, ws, o.AutoApply)
	}()
	return t, nil
}

// AssignWait hands an agent a task and waits for its report: how a
// colleague asks for help.
func (e *Engine) AssignWait(ctx context.Context, o AssignOpts) (Task, error) {
	o.AutoApply = true
	t, ws, err := e.open(ctx, o)
	if err != nil {
		return t, err
	}
	return e.play(ctx, t, ws, true), nil
}

func (e *Engine) open(ctx context.Context, o AssignOpts) (Task, string, error) {
	brief := strings.TrimSpace(o.Brief)
	if brief == "" {
		return Task{}, "", errors.New("the task is empty")
	}
	p, err := e.st.GetAgentProfile(ctx, o.ProfileID)
	if err != nil {
		return Task{}, "", fmt.Errorf("no such agent: %s", o.ProfileID)
	}
	agentID := types.ID("")
	if e.DefaultAgent != nil {
		if agentID, err = e.DefaultAgent(ctx); err != nil {
			return Task{}, "", err
		}
	}
	title := titleOf(brief)
	s, err := e.sess.CreateIn(ctx, types.SpaceOffice, title, agentID, o.WorkspaceID)
	if err != nil {
		return Task{}, "", err
	}
	if err := e.st.PutSessionPersona(ctx, types.SessionPersona{SessionID: s.ID, ProfileID: p.ID, UpdatedAt: time.Now().UTC()}); err != nil {
		return Task{}, "", err
	}
	t := Task{
		ID: id.NewID(), ProfileID: p.ID, ProfileName: p.Name, SessionID: s.ID, Title: title, Brief: brief,
		From: o.From, WatchID: o.WatchID, ScheduleID: o.ScheduleID, MonitorID: o.MonitorID,
		HandoffID: o.HandoffID, CarryFrom: o.CarryFrom, Chain: o.Chain, WorkspaceID: o.WorkspaceID, Origin: o.Origin, Status: StatusRunning, CreatedAt: time.Now().UTC(),
		// a colleague's request is read by the colleague, not the user
		Seen: o.From != "",
	}
	if o.From != "" {
		if f, err := e.st.GetAgentProfile(ctx, o.From); err == nil {
			t.FromName = f.Name
		}
	}
	if err := e.save(ctx, t); err != nil {
		return t, "", err
	}
	ws := ""
	if e.WorkspacePath != nil && o.WorkspaceID != "" {
		ws = e.WorkspacePath(ctx, o.WorkspaceID)
	}
	return t, ws, nil
}

// play runs a task to its end and records how it went.
func (e *Engine) play(ctx context.Context, t Task, workspace string, autoApply bool) Task {
	s, _ := e.st.GetSession(ctx, t.SessionID)
	brief := t.Brief
	extra := taskRules
	// in its own checkout the project lives elsewhere: paths follow it
	iso, workDir := e.isolate(&t, workspace)
	if iso != nil {
		t.Isolation = iso
		_ = e.save(context.Background(), t)
		brief = strings.ReplaceAll(brief, workspace, workDir)
		extra += "\nYou work in your own copy of the project (your workspace); your changes are kept for the user to review. Do not commit, push or switch branches."
	}
	msg := "Task from the user:\n" + brief
	if t.FromName != "" {
		msg = "Task from your colleague " + t.FromName + ":\n" + brief
	}
	var res agent.RunResult
	var err error
	if e.Run == nil {
		err = errors.New("no runner")
	} else {
		res, err = e.Run(ctx, agent.RunRequest{
			AgentID: s.AgentID, SessionID: t.SessionID, WorkspaceID: s.WorkspaceID, Workspace: workDir,
			UserMessage: msg, SystemExtra: extra, MaxTurns: taskTurns,
		})
	}
	switch {
	case err != nil && (ctx.Err() != nil || errors.Is(err, context.Canceled)):
		t.Status, t.Error = StatusStopped, "stopped"
	case err != nil:
		t.Status, t.Error = StatusFailed, err.Error()
	default:
		t.Status = StatusDone
	}
	t.Report = clipTail(strings.TrimSpace(res.Assistant), reportClip)
	t.EndedAt = time.Now().UTC()
	// the run may have been stopped from the app while it recorded
	if cur, gerr := e.Get(context.Background(), t.ID); gerr == nil && cur.Status == StatusStopped {
		t.Status, t.Error = StatusStopped, "stopped"
	}
	e.settle(&t, autoApply)
	_ = e.save(context.Background(), t)
	// paired agents take finished work further
	e.handOn(t)
	// the look back runs apart: a colleague waiting on this need not wait
	// for it too
	if e.Ask != nil {
		e.wg.Add(1)
		go func(done Task) {
			defer e.wg.Done()
			e.reflect(done)
		}(t)
	}
	// saved once, before handing on: a later save here would undo what
	// the handed task records on this one (its work carried on)
	return t
}

// taskRules set a task's terms: work on your own, no one to ask, a report
// at the end.
const taskRules = "## A task handed to you\n" +
	"You were given this task to do on your own; nobody is in the conversation to answer questions. " +
	"Where something is unclear, make the sensible choice and say what you assumed. " +
	"Check your work (run the build or the tests when there are any) before you call it done. " +
	"Finish with a short report: what you did, the files you changed, how you checked it, and anything left undone."

func (e *Engine) save(ctx context.Context, t Task) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if err := e.st.PutStaffTask(ctx, t.ID, t.ProfileID, t.SessionID, string(b), t.CreatedAt); err != nil {
		return err
	}
	if e.bus != nil {
		e.bus.Publish(types.Event{Type: EventUpdated, Topic: "staff", Timestamp: time.Now().UTC(), Payload: map[string]any{
			"id": string(t.ID), "profileId": string(t.ProfileID), "sessionId": string(t.SessionID),
			"title": t.Title, "status": string(t.Status), "from": string(t.From), "name": t.ProfileName,
		}})
	}
	return nil
}

func (e *Engine) Get(ctx context.Context, taskID types.ID) (Task, error) {
	body, err := e.st.GetStaffTask(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	var t Task
	return t, json.Unmarshal([]byte(body), &t)
}

// Tasks lists an agent's tasks, newest first.
func (e *Engine) Tasks(ctx context.Context, profileID types.ID, limit int) ([]Task, error) {
	bodies, err := e.st.ListStaffTasks(ctx, profileID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(bodies))
	for _, b := range bodies {
		var t Task
		if json.Unmarshal([]byte(b), &t) == nil {
			out = append(out, t)
		}
	}
	return out, nil
}

// Recover closes the tasks a stopped daemon left running: their run is
// gone, so they are marked stopped (their conversation keeps the work).
func (e *Engine) Recover(ctx context.Context) {
	tasks, err := e.Tasks(ctx, "", 0)
	if err != nil {
		return
	}
	for _, t := range tasks {
		if t.Status == StatusRunning {
			t.Status, t.Error, t.EndedAt = StatusStopped, "the app stopped while it worked", time.Now().UTC()
			// what it changed before that waits for the user
			if t.Isolation != nil && t.Isolation.State == IsoWorking {
				e.settle(&t, false)
			}
			_ = e.save(ctx, t)
		}
	}
}

// Seen marks a task's report read.
func (e *Engine) Seen(ctx context.Context, taskID types.ID) (Task, error) {
	t, err := e.Get(ctx, taskID)
	if err != nil || t.Seen || !t.done() {
		return t, err
	}
	t.Seen = true
	return t, e.save(ctx, t)
}

// Stop ends a running task at its next step.
func (e *Engine) Stop(ctx context.Context, taskID types.ID) (Task, error) {
	t, err := e.Get(ctx, taskID)
	if err != nil || t.done() {
		return t, err
	}
	t.Status, t.Error, t.EndedAt = StatusStopped, "stopped", time.Now().UTC()
	if err := e.save(ctx, t); err != nil {
		return t, err
	}
	if e.Cancel != nil {
		e.Cancel(t.SessionID)
	}
	return t, nil
}

// Members is the whole team as the Agent space shows it.
func (e *Engine) Members(ctx context.Context) ([]Member, error) {
	profiles, err := e.st.ListAgentProfiles(ctx)
	if err != nil {
		return nil, err
	}
	// which agent each live conversation belongs to
	live := map[types.ID]types.Session{}
	if e.Running != nil {
		if list, err := e.sess.List(ctx, ""); err == nil {
			for _, s := range list {
				if s.Space != types.SpaceOffice || !e.Running(s.ID) {
					continue
				}
				if sp, err := e.st.GetSessionPersona(ctx, s.ID); err == nil && sp.ProfileID != "" {
					if _, ok := live[sp.ProfileID]; !ok {
						live[sp.ProfileID] = s
					}
				}
			}
		}
	}
	out := make([]Member, 0, len(profiles))
	for _, p := range profiles {
		m := Member{Profile: p, State: "idle", Tasks: []Task{}, Notes: []types.MemoryEntry{}}
		// none is an empty list, never null: the app reads their length
		if tasks, err := e.Tasks(ctx, p.ID, recentTasks); err == nil && tasks != nil {
			m.Tasks = tasks
		}
		if notes, err := e.Notes(ctx, p.ID); err == nil && notes != nil {
			m.Notes = notes
		}
		for _, t := range m.Tasks {
			if t.Status == StatusRunning {
				m.State, m.Now, m.NowSession = "working", t.Title, t.SessionID
				break
			}
		}
		if m.State == "idle" {
			if s, ok := live[p.ID]; ok {
				m.State, m.Now, m.NowSession = "working", s.Title, s.ID
			}
		}
		if m.State == "idle" {
			for _, t := range m.Tasks {
				if t.done() && !t.Seen {
					m.State = "report"
					break
				}
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// ── notes: what an agent remembers across its conversations ─────────────────

func (e *Engine) Notes(ctx context.Context, profileID types.ID) ([]types.MemoryEntry, error) {
	return e.st.ListMemory(ctx, types.MemProfile, profileID)
}

// AddNote saves one line to an agent's notes. Its key is its first words, so
// saying the same thing again replaces the note instead of adding a twin.
func (e *Engine) AddNote(ctx context.Context, profileID types.ID, text string) (types.MemoryEntry, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return types.MemoryEntry{}, errors.New("the note is empty")
	}
	if len([]rune(text)) > noteRunes {
		text = string([]rune(text)[:noteRunes])
	}
	n := types.MemoryEntry{ID: id.NewID(), Scope: types.MemProfile, ScopeID: profileID, Key: keyOf(text), Content: text, CreatedAt: time.Now().UTC()}
	return n, e.st.PutMemory(ctx, n)
}

func (e *Engine) DeleteNote(ctx context.Context, noteID types.ID) error {
	return e.st.DeleteMemory(ctx, noteID)
}

// Notes ride along on every turn: kept short, and only the latest many.
const (
	noteRunes = 280
	notesKept = 30
)

// ── what an agent is told about itself and its team ─────────────────────────

// ProfileOf is the Office agent a session talks to; empty when none.
func (e *Engine) ProfileOf(ctx context.Context, sessionID types.ID) types.ID {
	sp, err := e.st.GetSessionPersona(ctx, sessionID)
	if err != nil {
		return ""
	}
	return sp.ProfileID
}

// askedByColleague reports whether a session is a colleague's request: its
// agent does the work itself and does not pass it on (one level deep).
func (e *Engine) askedByColleague(ctx context.Context, sessionID types.ID) bool {
	body, err := e.st.StaffTaskBySession(ctx, sessionID)
	if err != nil {
		return false
	}
	var t Task
	return json.Unmarshal([]byte(body), &t) == nil && t.From != ""
}

// CanAsk reports whether a session's agent may ask a colleague for help.
func (e *Engine) CanAsk(ctx context.Context, sessionID types.ID) bool {
	me := e.ProfileOf(ctx, sessionID)
	if me == "" || e.askedByColleague(ctx, sessionID) {
		return false
	}
	return len(e.colleagues(ctx, me)) > 0
}

func (e *Engine) colleagues(ctx context.Context, me types.ID) []types.AgentProfile {
	all, err := e.st.ListAgentProfiles(ctx)
	if err != nil {
		return nil
	}
	var out []types.AgentProfile
	for _, p := range all {
		if p.ID != me {
			out = append(out, p)
		}
	}
	return out
}

// Brief is what a session's Office agent is told about itself: who it is,
// its notes, and the colleagues it can ask. Empty for any other session.
func (e *Engine) Brief(ctx context.Context, sessionID types.ID) string {
	me := e.ProfileOf(ctx, sessionID)
	if me == "" {
		return ""
	}
	p, err := e.st.GetAgentProfile(ctx, me)
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Who you are\nYou are " + p.Name)
	if p.Title != "" {
		b.WriteString(", " + p.Title)
	}
	b.WriteString(", on the user's team. You keep notes of your own across conversations: save with " + NoteName +
		" what you should remember next time (the user's preferences, decisions, where things live), one line each.")
	if notes, err := e.Notes(ctx, me); err == nil && len(notes) > 0 {
		if len(notes) > notesKept {
			notes = notes[len(notes)-notesKept:]
		}
		b.WriteString("\n\n## Your notes\n")
		for _, n := range notes {
			b.WriteString("- " + n.Content + "\n")
		}
	}
	if !e.askedByColleague(ctx, sessionID) {
		if cs := e.colleagues(ctx, me); len(cs) > 0 {
			b.WriteString("\n\n## Your colleagues\nWhen a part of the job is another's field, hand it to them with " + AskName +
				" and use their report; do the rest yourself. Give them everything the part needs: they cannot see this conversation.\n")
			for _, c := range cs {
				line := "- " + c.Name
				if c.Title != "" {
					line += " — " + c.Title
				}
				b.WriteString(line + "\n")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// findColleague matches a name or a title, the surest match first.
func (e *Engine) findColleague(ctx context.Context, me types.ID, q string) (types.AgentProfile, bool) {
	q = strings.ToLower(strings.TrimSpace(q))
	cs := e.colleagues(ctx, me)
	if q == "" {
		return types.AgentProfile{}, false
	}
	for _, c := range cs {
		if strings.ToLower(c.Name) == q || (c.Title != "" && strings.ToLower(c.Title) == q) {
			return c, true
		}
	}
	for _, c := range cs {
		if n := strings.ToLower(c.Name); strings.Contains(n, q) || strings.Contains(q, n) {
			return c, true
		}
	}
	for _, c := range cs {
		if t := strings.ToLower(c.Title); t != "" && strings.Contains(t, q) {
			return c, true
		}
	}
	return types.AgentProfile{}, false
}

func titleOf(brief string) string {
	line := strings.TrimSpace(strings.SplitN(brief, "\n", 2)[0])
	r := []rune(line)
	if len(r) > titleRunes {
		return strings.TrimSpace(string(r[:titleRunes])) + "…"
	}
	return line
}

func keyOf(text string) string {
	words := strings.Fields(strings.ToLower(text))
	if len(words) > 4 {
		words = words[:4]
	}
	k := strings.Join(words, " ")
	if r := []rune(k); len(r) > 40 {
		k = string(r[:40])
	}
	return k
}

// clipTail keeps a report's end, where an agent writes its result.
func clipTail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	tail := string(r[len(r)-(n-1):])
	if i := strings.Index(tail, "\n\n"); i >= 0 && i < len(tail)/3 {
		tail = tail[i+2:]
	}
	return "…" + strings.TrimSpace(tail)
}
