package staff

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Kayra-ML/rove/internal/types"
)

// A task given to an agent works in a checkout of its own when the project
// is a git repository: it starts from the tree as the user has it now
// (uncommitted work included), and what it changes waits there for the
// user — to apply to the project, to throw away, or to open as a pull
// request. An agent working in the background never writes into the user's
// folder by itself. A colleague's request is the exception: the colleague
// waiting on it needs the work, so it lands as soon as it is done.

// Isolation is a task's own checkout and what became of its work.
type Isolation struct {
	Repo   string `json:"repo"`
	Path   string `json:"path,omitempty"`
	Branch string `json:"branch,omitempty"`
	Base   string `json:"base,omitempty"`
	// State: working, review (waits for the user), applied, discarded, pr
	// (opened as a pull request), empty (changed nothing).
	State   string   `json:"state"`
	Files   []string `json:"files,omitempty"`
	Added   int      `json:"added"`
	Removed int      `json:"removed"`
	PR      string   `json:"pr,omitempty"`
	// Note says why something did not go as planned.
	Note string `json:"note,omitempty"`
}

const (
	IsoWorking   = "working"
	IsoReview    = "review"
	IsoApplied   = "applied"
	IsoDiscarded = "discarded"
	IsoPR        = "pr"
	IsoEmpty     = "empty"
	// carried: a paired agent took the work on; its task holds it now
	IsoCarried = "carried"
)

// isolate opens a task's checkout, or returns nil when the project is not a
// git repository (the task then works in the folder, as before).
func (e *Engine) isolate(t *Task, workspace string) (*Isolation, string) {
	if e.Git == nil || e.WorktreeRoot == "" || workspace == "" || !e.Git.IsRepo(workspace) {
		return nil, workspace
	}
	// handed on: start from the colleague's changes, and keep their base,
	// so the review shows both pieces of work together
	if prev, ok := e.carried(t); ok {
		start, err := e.Git.Snapshot(prev.Isolation.Path)
		if err == nil {
			iso := &Isolation{Repo: prev.Isolation.Repo, Path: filepath.Join(e.WorktreeRoot, string(t.ID)), Branch: "rove/agent-" + string(t.ID), Base: prev.Isolation.Base, State: IsoWorking}
			if err := e.Git.AddWorktree(iso.Repo, iso.Path, iso.Branch, start); err == nil {
				_ = e.Git.DropWorktree(prev.Isolation.Repo, prev.Isolation.Path, prev.Isolation.Branch)
				prev.Isolation.State, prev.Isolation.Path = IsoCarried, ""
				prev.Isolation.Note = "carried on by " + t.ProfileName
				_ = e.save(context.Background(), prev)
				return iso, iso.Path
			}
		}
	}
	base, err := e.Git.Snapshot(workspace)
	if err != nil {
		return nil, workspace
	}
	iso := &Isolation{Repo: workspace, Path: filepath.Join(e.WorktreeRoot, string(t.ID)), Branch: "rove/agent-" + string(t.ID), Base: base, State: IsoWorking}
	if err := e.Git.AddWorktree(workspace, iso.Path, iso.Branch, base); err != nil {
		return nil, workspace
	}
	return iso, iso.Path
}

// carried is the task a handed-on task starts from, when its changes still
// wait for review.
func (e *Engine) carried(t *Task) (Task, bool) {
	if t.CarryFrom == "" {
		return Task{}, false
	}
	prev, err := e.Get(context.Background(), t.CarryFrom)
	if err != nil || prev.Isolation == nil || prev.Isolation.State != IsoReview || prev.Isolation.Path == "" {
		return Task{}, false
	}
	return prev, true
}

// settle looks at what a finished task changed: nothing — the checkout
// goes; a colleague's request — it lands; else it waits for the user.
func (e *Engine) settle(t *Task, autoApply bool) {
	iso := t.Isolation
	if iso == nil || e.Git == nil {
		return
	}
	patch, added, removed, files, err := e.Git.WorkPatch(iso.Path, iso.Base)
	if err != nil {
		iso.State, iso.Note = IsoReview, "could not read the changes: "+firstLine(err.Error())
		return
	}
	iso.Files, iso.Added, iso.Removed = files, added, removed
	if len(files) == 0 {
		_ = e.Git.DropWorktree(iso.Repo, iso.Path, iso.Branch)
		iso.State, iso.Path = IsoEmpty, ""
		return
	}
	iso.State = IsoReview
	if !autoApply || t.Status != StatusDone {
		return
	}
	e.applyMu.Lock()
	defer e.applyMu.Unlock()
	if err := e.Git.ApplyPatch(iso.Repo, patch); err != nil {
		iso.Note = "clashes with other changes to the same lines: " + firstLine(err.Error())
		return
	}
	_ = e.Git.DropWorktree(iso.Repo, iso.Path, iso.Branch)
	iso.State, iso.Path = IsoApplied, ""
}

// reviewable finds a task whose work waits for the user.
func (e *Engine) reviewable(ctx context.Context, taskID types.ID) (Task, error) {
	t, err := e.Get(ctx, taskID)
	if err != nil {
		return t, err
	}
	if t.Isolation == nil || t.Isolation.State != IsoReview || t.Isolation.Path == "" {
		return t, errors.New("this task has no changes waiting")
	}
	if !t.done() {
		return t, errors.New("it is still working")
	}
	return t, nil
}

// Diff is a task's waiting changes as a patch, clipped for reading.
func (e *Engine) Diff(ctx context.Context, taskID types.ID) (string, error) {
	t, err := e.reviewable(ctx, taskID)
	if err != nil {
		return "", err
	}
	patch, _, _, _, err := e.Git.WorkPatch(t.Isolation.Path, t.Isolation.Base)
	if err != nil {
		return "", err
	}
	const max = 60000
	if len(patch) > max {
		patch = patch[:max] + "\n… (cut)"
	}
	return patch, nil
}

// Apply lays a task's changes onto the project, uncommitted.
func (e *Engine) Apply(ctx context.Context, taskID types.ID) (Task, error) {
	t, err := e.reviewable(ctx, taskID)
	if err != nil {
		return t, err
	}
	iso := t.Isolation
	e.applyMu.Lock()
	patch, _, _, _, err := e.Git.WorkPatch(iso.Path, iso.Base)
	if err == nil {
		err = e.Git.ApplyPatch(iso.Repo, patch)
	}
	e.applyMu.Unlock()
	if err != nil {
		iso.Note = "clashes with other changes to the same lines: " + firstLine(err.Error())
		_ = e.save(ctx, t)
		return t, fmt.Errorf("%s", iso.Note)
	}
	_ = e.Git.DropWorktree(iso.Repo, iso.Path, iso.Branch)
	iso.State, iso.Path, iso.Note = IsoApplied, "", ""
	t.Seen = true
	return t, e.save(ctx, t)
}

// Discard throws a task's changes away.
func (e *Engine) Discard(ctx context.Context, taskID types.ID) (Task, error) {
	t, err := e.reviewable(ctx, taskID)
	if err != nil {
		return t, err
	}
	iso := t.Isolation
	_ = e.Git.DropWorktree(iso.Repo, iso.Path, iso.Branch)
	iso.State, iso.Path, iso.Note = IsoDiscarded, "", ""
	t.Seen = true
	return t, e.save(ctx, t)
}

// OpenPR opens a task's changes as a pull request. The checkout it worked
// in starts from a snapshot holding the user's uncommitted work, which must
// not go out with it: the PR's branch starts from the project's last
// commit instead, and only the task's own changes go onto it. Pushing and
// opening use the user's git remote and gh sign-in.
func (e *Engine) OpenPR(ctx context.Context, taskID types.ID) (Task, error) {
	t, err := e.reviewable(ctx, taskID)
	if err != nil {
		return t, err
	}
	iso := t.Isolation
	patch, _, _, _, err := e.Git.WorkPatch(iso.Path, iso.Base)
	if err != nil {
		return t, err
	}
	base, _ := e.Git.CurrentBranch(iso.Repo)
	base = strings.TrimSpace(base)
	branch := "rove/" + slug(t.Title) + "-" + string(t.ID)[:6]
	tmp := iso.Path + "-pr"
	fail := func(step string, err error) (Task, error) {
		// the branch was never pushed (or the push failed): it goes too
		_ = e.Git.DropWorktree(iso.Repo, tmp, branch)
		iso.Note = step + ": " + firstLine(err.Error())
		_ = e.save(ctx, t)
		return t, fmt.Errorf("%s", iso.Note)
	}
	if err := e.Git.AddWorktree(iso.Repo, tmp, branch, "HEAD"); err != nil {
		return fail("could not start the branch", err)
	}
	if err := e.Git.ApplyPatch(tmp, patch); err != nil {
		return fail("the changes need work that is not committed yet", err)
	}
	if err := e.Git.CommitAll(tmp, t.Title); err != nil {
		return fail("could not commit", err)
	}
	if err := e.Git.PushBranch(tmp, "origin", branch); err != nil {
		return fail("could not push", err)
	}
	body := t.Report
	if body == "" {
		body = t.Brief
	}
	url, err := e.Git.CreatePR(tmp, t.Title, body+"\n\n— opened by "+t.ProfileName+" (Rove Code)", base)
	if err != nil {
		return fail("could not open the pull request", err)
	}
	// the pushed branch stays; both checkouts go
	_ = e.Git.RemoveWorktree(iso.Repo, tmp)
	_ = e.Git.DropWorktree(iso.Repo, iso.Path, iso.Branch)
	iso.State, iso.Path, iso.PR, iso.Note = IsoPR, "", url, ""
	t.Seen = true
	return t, e.save(ctx, t)
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if b.Len() >= 40 {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "task"
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
