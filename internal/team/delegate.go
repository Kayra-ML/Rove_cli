package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/gitwt"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

const DelegateName = "team_delegate"

// A subagent's report is clipped before it reaches the lead: enough to merge
// and decide, not a replay of its whole turn.
const reportClip = 1200

// How many tasks of one call may run at once, and how many a call may carry
// at all. A lead that splits a job into twenty parts would otherwise start
// twenty model runs in the same instant — the bill and the provider's rate
// limit both arrive at once, and nothing is faster for it. The rest wait
// their turn rather than being dropped.
const (
	maxParallel    = 4
	maxAssignments = 12
)

// childTurns is a subagent's budget of model turns. A chat's own runs stop
// at 12, which is a conversation's pace; a subagent is sent to finish a
// piece of work on its own and needs room to read, change and check it.
const childTurns = 40

// Runner starts one agent run; *agent.Runtime.Run fits.
type Runner func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error)

// Subagents runs the work a lead hands out. The lead's tool calls into it,
// and so do the app's buttons: stop one, steer one, apply or discard the
// work one left in its own checkout.
//
// The model is Hermes Agent's delegate_task: every subagent starts with a
// fresh context holding only its task, runs with a budget of its own, and
// only its short report comes back. Orca's contribution is the checkout per
// agent — parallel tasks never write the same tree, so they cannot trample
// each other, and what they did comes back to the user's tree afterwards.
type Subagents struct {
	Team *Team
	Run  Runner
	// Roster is what the app shows; nil keeps one of its own.
	Roster *Roster
	// Git and WorktreeRoot turn on a checkout per task for parallel calls in
	// a git project; with either unset, tasks share the workspace and the
	// file claims keep them off each other's files.
	Git          *gitwt.Manager
	WorktreeRoot string
	// Cancel stops a session's run; Steer hands it words for its next turn.
	Cancel func(types.ID)
	Steer  func(types.ID, string) bool

	once    sync.Once
	applyMu sync.Mutex
}

func (s *Subagents) roster() *Roster {
	s.once.Do(func() {
		if s.Roster == nil {
			s.Roster = NewRoster(nil)
		}
	})
	return s.Roster
}

// Delegate is the lead's tool.
type Delegate struct{ *Subagents }

func (Delegate) Name() string { return DelegateName }

func (Delegate) Description() string {
	return "Hand tasks to subagents that work on their own and report back. " +
		"Each starts with a fresh context: it knows nothing of this conversation, so give every task what it needs " +
		"(absolute file paths, the constraint, the command that proves it works, what done looks like). " +
		"Tasks in one call run in parallel, each in its own copy of the project when the project is a git repo; " +
		"their changes are brought back to the workspace afterwards. Only each task's short report comes back."
}

func (Delegate) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"tasks":{"type":"array","items":{"type":"object","properties":{` +
		`"goal":{"type":"string","description":"what to achieve, in one or two sentences"},` +
		`"context":{"type":"string","description":"everything the subagent needs: paths, constraints, how to check it"}` +
		`},"required":["goal"]}}},"required":["tasks"]}`)
}

// Delegating touches nothing itself; each subagent's own tools are checked
// when they run.
func (Delegate) RequiredPermission() types.PermissionAction { return types.PermFilesystem }

type assignment struct {
	Goal    string `json:"goal"`
	Context string `json:"context"`
	// the older shape: {task}
	Task string `json:"task"`
}

func (a assignment) goal() string {
	if g := strings.TrimSpace(a.Goal); g != "" {
		return g
	}
	return strings.TrimSpace(a.Task)
}

func (d Delegate) Call(ctx context.Context, tc tool.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Tasks []assignment `json:"tasks"`
		// older shapes, still accepted: {assignments:[{task}]} and a lone
		// {task} or {goal, context}
		Assignments []assignment `json:"assignments"`
		assignment
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tool.Result{Content: "bad arguments: " + err.Error(), IsError: true}, nil
	}
	tasks := append(append([]assignment{}, in.Tasks...), in.Assignments...)
	if in.assignment.goal() != "" {
		tasks = append(tasks, in.assignment)
	}
	if len(tasks) == 0 {
		return tool.Result{Content: "no tasks", IsError: true}, nil
	}
	if len(tasks) > maxAssignments {
		return tool.Result{
			Content: fmt.Sprintf("too many assignments (%d); hand out at most %d at a time and call again with the rest",
				len(tasks), maxAssignments),
			IsError: true,
		}, nil
	}
	for i, a := range tasks {
		if a.goal() == "" {
			return tool.Result{Content: fmt.Sprintf("task %d has no goal", i+1), IsError: true}, nil
		}
	}
	return d.run(ctx, tc, tasks)
}

// target is where one task runs.
type target struct {
	session types.Session
	err     string
}

func (s *Subagents) run(ctx context.Context, tc tool.Context, in []assignment) (tool.Result, error) {
	parent, err := s.Team.st.GetSession(ctx, tc.SessionID)
	if err != nil {
		return tool.Result{Content: "no such chat: " + err.Error(), IsError: true}, nil
	}
	// Each task gets a channel of its own.
	targets := make([]target, len(in))
	for i, a := range in {
		ch, err := s.Team.sess.CreateChildIn(ctx, parent, types.SpaceWorker, "Subagent · "+clip(a.goal(), 48))
		if err != nil {
			targets[i].err = "could not open a channel: " + err.Error()
			continue
		}
		targets[i] = target{session: ch}
	}

	// Parallel tasks in a git project get a checkout each, started from the
	// tree as the user has it now — uncommitted work included.
	repo, base, isoNote := s.isolation(tc.Workspace, len(in))

	batch := string(id.NewID())
	tasks := make([]*Task, len(in))
	for i, a := range in {
		t := &Task{
			ID: string(id.NewID()), Batch: batch, Index: i + 1, Of: len(in),
			Parent: parent.ID, Goal: a.goal(), Status: StatusQueued,
		}
		if targets[i].err != "" {
			t.Status, t.Error, t.EndedAt = StatusFailed, targets[i].err, time.Now().UTC()
		} else {
			t.SessionID = targets[i].session.ID
		}
		tasks[i] = t
	}
	r := s.roster()
	r.add(parent.ID, tasks)

	gate := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for i := range in {
		if tasks[i].Status != StatusQueued {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			s.runOne(ctx, tc, tasks[i], targets[i], in[i], repo, base)
		}(i)
	}
	wg.Wait()

	return report(r, tasks, isoNote), nil
}

// isolation decides whether this call's tasks get checkouts of their own:
// only when more than one of them runs at once, and only in a git project
// that has a commit to start from. The note says why not, when not.
func (s *Subagents) isolation(workspace string, n int) (repo, base, note string) {
	if n < 2 || s.Git == nil || s.WorktreeRoot == "" || workspace == "" {
		return "", "", ""
	}
	if !s.Git.IsRepo(workspace) {
		return "", "", "the project is not a git repository, so the tasks shared one folder"
	}
	snap, err := s.Git.Snapshot(workspace)
	if err != nil {
		return "", "", "no separate copies (" + firstLine(err.Error()) + "), so the tasks shared one folder"
	}
	return workspace, snap, ""
}

func (s *Subagents) runOne(ctx context.Context, tc tool.Context, t *Task, tg target, a assignment, repo, base string) {
	r := s.roster()
	// stopped while it was still waiting for a slot
	if snap, _ := r.Snapshot(t.ID); snap.Status != StatusQueued {
		return
	}
	workDir := tc.Workspace
	var iso *Isolation
	if repo != "" {
		dest := filepath.Join(s.WorktreeRoot, t.ID)
		branch := "rove/sub-" + t.ID
		if err := s.Git.AddWorktree(repo, dest, branch, base); err == nil {
			workDir = dest
			iso = &Isolation{Repo: repo, Path: dest, Branch: branch, Base: base}
		}
	}
	r.update(t, func(t *Task) {
		t.Status, t.StartedAt, t.Worktree = StatusRunning, time.Now().UTC(), iso
	})

	req := agent.RunRequest{
		AgentID: tg.session.AgentID, SessionID: tg.session.ID,
		WorkspaceID: tc.WorkspaceID, Workspace: workDir,
		UserMessage: brief(a, tc.Workspace, workDir),
		SystemExtra: rules(iso != nil),
		MaxTurns:    childTurns,
	}
	res, err := s.Run(ctx, req)

	status := StatusCompleted
	errText := ""
	switch {
	case err != nil && (ctx.Err() != nil || errors.Is(err, context.Canceled)):
		status, errText = StatusInterrupted, "stopped"
	case err != nil:
		status, errText = StatusFailed, err.Error()
	case !res.Done:
		status = StatusMaxTurns
	}
	if snap, _ := r.Snapshot(t.ID); snap.Status == StatusInterrupted {
		status, errText = StatusInterrupted, "stopped"
	}
	files := uniq(res.FilesEdited)
	if iso != nil {
		files = nil // the checkout's own diff names them, relative to the project
		// git runs on a copy, outside the roster's lock, so the app can keep
		// reading the roster while a patch lands; the result goes in after
		out := *iso
		s.settle(&out, status == StatusCompleted)
		r.update(t, func(t *Task) { t.Worktree = &out })
	}
	r.update(t, func(t *Task) {
		t.Status, t.Error, t.EndedAt = status, errText, time.Now().UTC()
		t.Turns, t.Summary = res.Turns, clipTail(strings.TrimSpace(res.Assistant), reportClip)
		if files != nil {
			t.Files = files
		}
	})
}

// settle brings a finished checkout's work back to the user's tree. Only a
// task that finished gets this on its own; a stopped or cut-short one leaves
// its work where it is, for someone to look at before it lands. Checkouts
// that changed nothing are simply removed. Patches land one at a time.
func (s *Subagents) settle(iso *Isolation, finished bool) {
	patch, added, removed, files, err := s.Git.WorkPatch(iso.Path, iso.Base)
	if err != nil {
		iso.Pending, iso.Reason = "could not read the changes: "+firstLine(err.Error()), ReasonUnreadable
		return
	}
	iso.Added, iso.Removed, iso.Files = added, removed, files
	if len(files) == 0 {
		_ = s.Git.DropWorktree(iso.Repo, iso.Path, iso.Branch)
		iso.Applied, iso.Path = true, ""
		return
	}
	if !finished {
		iso.Pending, iso.Reason = "the task did not finish; its changes wait here to be looked at", ReasonUnfinished
		return
	}
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	if err := s.Git.ApplyPatch(iso.Repo, patch); err != nil {
		iso.Pending, iso.Reason = "clashes with other changes to the same lines: "+firstLine(err.Error()), ReasonClash
		return
	}
	_ = s.Git.DropWorktree(iso.Repo, iso.Path, iso.Branch)
	iso.Applied, iso.Path = true, ""
}

// brief is what a subagent is told. It has no conversation to lean on, so
// the goal and the lead's context are all of it. In its own checkout the
// project lives at another path: the lead's paths are moved there, or it
// would write the user's tree directly and the separation would be for
// nothing.
func brief(a assignment, workspace, workDir string) string {
	goal, extra := a.goal(), strings.TrimSpace(a.Context)
	if workspace != "" && workDir != workspace {
		goal = strings.ReplaceAll(goal, workspace, workDir)
		extra = strings.ReplaceAll(extra, workspace, workDir)
	}
	var b strings.Builder
	b.WriteString("Task from the lead:\n")
	b.WriteString(goal)
	if extra != "" {
		b.WriteString("\n\nContext:\n")
		b.WriteString(extra)
	}
	return b.String()
}

// rules set a subagent's terms: one task, no one to ask, a report at the end.
func rules(isolated bool) string {
	var b strings.Builder
	b.WriteString("## You are a subagent\n")
	b.WriteString("A lead agent gave you one task. You cannot see its conversation; the task holds everything you have. " +
		"Do that task and nothing beyond it. Nobody will answer a question: where something is unclear, make the sensible choice and say what you assumed. ")
	if isolated {
		b.WriteString("You work in your own copy of the project (your workspace, below); other subagents work in theirs at the same time. " +
			"Your changes are brought back to the project when you finish, so do not commit, push or switch branches. ")
	}
	b.WriteString("Finish with a short report: what you did, the files you changed, how you checked it (the command you ran and its result), and anything left undone.")
	return b.String()
}

// report is the tool's answer to the lead: one block per task, in the order
// they were given, with enough to merge and decide what to check.
func report(r *Roster, tasks []*Task, note string) tool.Result {
	var b strings.Builder
	failed := 0
	for i, t := range tasks {
		snap, _ := r.Snapshot(t.ID)
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "TASK %d/%d · subagent · %s", snap.Index, snap.Of, snap.Status)
		if snap.Turns > 0 {
			fmt.Fprintf(&b, " · %d turns", snap.Turns)
		}
		b.WriteString("\n")
		switch snap.Status {
		case StatusFailed:
			failed++
			b.WriteString("failed: " + snap.Error)
		case StatusInterrupted:
			b.WriteString("stopped before it finished.")
			if snap.Summary != "" {
				b.WriteString(" Last words:\n" + snap.Summary)
			}
		case StatusMaxTurns:
			b.WriteString("ran out of turns before finishing; the work is partial.")
			if snap.Summary != "" {
				b.WriteString("\n" + snap.Summary)
			}
		default:
			b.WriteString(snap.Summary)
		}
		if w := snap.Worktree; w != nil && len(w.Files) > 0 {
			fmt.Fprintf(&b, "\nChanges: %s (+%d −%d)", strings.Join(w.Files, ", "), w.Added, w.Removed)
			if w.Applied {
				b.WriteString(" — applied to the workspace.")
			} else {
				fmt.Fprintf(&b, " — NOT applied: %s. Kept at %s for the user to review.", w.Pending, w.Path)
			}
		} else if len(snap.Files) > 0 {
			b.WriteString("\nFiles: " + strings.Join(snap.Files, ", "))
		}
	}
	if note != "" {
		b.WriteString("\n\nNote: " + note + ".")
	}
	b.WriteString("\n\nThese are the subagents' own accounts. Check what matters before you call it done.")
	return tool.Result{Content: b.String(), IsError: failed == len(tasks)}
}

// ── what the app can do to a subagent ─────────────────────────────────────────

// Stop ends one task: a running one at its next step, a waiting one before
// it starts.
func (s *Subagents) Stop(taskID string) (Task, error) {
	r := s.roster()
	t, ok := r.Get(taskID)
	if !ok {
		return Task{}, errors.New("no such subagent")
	}
	snap, _ := r.Snapshot(taskID)
	if snap.Done() {
		return snap, nil
	}
	r.update(t, func(t *Task) {
		t.Status, t.Error = StatusInterrupted, "stopped"
		if t.StartedAt.IsZero() {
			t.EndedAt = time.Now().UTC()
		}
	})
	if snap.Status == StatusRunning && s.Cancel != nil {
		s.Cancel(snap.SessionID)
	}
	out, _ := r.Snapshot(taskID)
	return out, nil
}

// SteerTask hands a running task words for its next turn.
func (s *Subagents) SteerTask(taskID, text string) error {
	snap, ok := s.roster().Snapshot(taskID)
	if !ok {
		return errors.New("no such subagent")
	}
	if snap.Status != StatusRunning || s.Steer == nil || !s.Steer(snap.SessionID, text) {
		return errors.New("that subagent is not running")
	}
	return nil
}

// Apply brings a checkout's waiting work into the user's tree — after a
// clash was sorted out by hand, or for a task that did not finish but whose
// work the user wants anyway.
func (s *Subagents) Apply(taskID string) (Task, error) {
	r := s.roster()
	t, ok := r.Get(taskID)
	if !ok {
		return Task{}, errors.New("no such subagent")
	}
	snap, _ := r.Snapshot(taskID)
	w := snap.Worktree
	if w == nil || w.Applied || w.Path == "" {
		return snap, errors.New("nothing waiting to be applied")
	}
	if !snap.Done() {
		return snap, errors.New("it is still working")
	}
	s.applyMu.Lock()
	patch, added, removed, files, err := s.Git.WorkPatch(w.Path, w.Base)
	if err == nil {
		err = s.Git.ApplyPatch(w.Repo, patch)
	}
	s.applyMu.Unlock()
	if err != nil {
		r.update(t, func(t *Task) {
			t.Worktree.Pending, t.Worktree.Reason = "still clashes: "+firstLine(err.Error()), ReasonClash
		})
		out, _ := r.Snapshot(taskID)
		return out, err
	}
	_ = s.Git.DropWorktree(w.Repo, w.Path, w.Branch)
	r.update(t, func(t *Task) {
		t.Worktree.Applied, t.Worktree.Pending, t.Worktree.Reason, t.Worktree.Path = true, "", "", ""
		t.Worktree.Added, t.Worktree.Removed, t.Worktree.Files = added, removed, files
	})
	out, _ := r.Snapshot(taskID)
	return out, nil
}

// Discard throws a checkout's waiting work away.
func (s *Subagents) Discard(taskID string) (Task, error) {
	r := s.roster()
	t, ok := r.Get(taskID)
	if !ok {
		return Task{}, errors.New("no such subagent")
	}
	snap, _ := r.Snapshot(taskID)
	w := snap.Worktree
	if w == nil || w.Applied || w.Path == "" {
		return snap, errors.New("nothing waiting to be discarded")
	}
	if !snap.Done() {
		return snap, errors.New("it is still working")
	}
	_ = s.Git.DropWorktree(w.Repo, w.Path, w.Branch)
	r.update(t, func(t *Task) {
		t.Worktree.Pending, t.Worktree.Reason, t.Worktree.Path = "discarded", ReasonDiscarded, ""
	})
	out, _ := r.Snapshot(taskID)
	return out, nil
}

// List is a chat's roster; a channel's id finds its chat's.
func (s *Subagents) List(ctx context.Context, sessionID types.ID) []Task {
	if ses, err := s.Team.st.GetSession(ctx, sessionID); err == nil && ses.ParentID != "" {
		sessionID = ses.ParentID
	}
	return s.roster().List(sessionID)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + " …"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// clipTail keeps a report's end, where an agent writes its result; the
// narration of how it got there is what goes.
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
