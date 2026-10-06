package team

import (
	"sort"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/types"
)

// EventSubagents fires on a chat's topic whenever one of its subagents
// changes state; the payload carries the chat's whole roster, so the app
// never has to stitch partial updates together.
const EventSubagents types.EventType = "subagent.updated"

// The states a delegated task goes through. A task that ran out of turns is
// kept apart from one that failed: its work is partial, not wrong.
const (
	StatusQueued      = "queued"
	StatusRunning     = "running"
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusInterrupted = "interrupted"
	StatusMaxTurns    = "max_turns"
)

// Isolation is a task's own checkout of the project, when it had one, and
// what became of the work done there.
type Isolation struct {
	Repo    string   `json:"repo"`
	Path    string   `json:"path"`
	Branch  string   `json:"branch"`
	Base    string   `json:"base"`
	Added   int      `json:"added"`
	Removed int      `json:"removed"`
	Files   []string `json:"files,omitempty"`
	// Applied: the changes are in the user's tree now and the checkout is
	// gone. Pending says why they are not, when they are not — in words for
	// the lead; Reason is the same as a code, for the app to put in the
	// user's language.
	Applied bool   `json:"applied"`
	Pending string `json:"pending,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Why a checkout's work did not land.
const (
	ReasonClash      = "clash"
	ReasonUnfinished = "unfinished"
	ReasonUnreadable = "unreadable"
	ReasonDiscarded  = "discarded"
)

// Task is one piece of work the lead handed out, as the roster shows it.
type Task struct {
	ID    string `json:"id"`
	Batch string `json:"batch"`
	// Index and Of place it in its call: task 2 of 3.
	Index     int        `json:"index"`
	Of        int        `json:"of"`
	Parent    types.ID   `json:"parentId"`
	SessionID types.ID   `json:"sessionId"`
	Goal      string     `json:"goal"`
	Status    string     `json:"status"`
	StartedAt time.Time  `json:"startedAt,omitempty"`
	EndedAt   time.Time  `json:"endedAt,omitempty"`
	Turns     int        `json:"turns,omitempty"`
	Summary   string     `json:"summary,omitempty"`
	Error     string     `json:"error,omitempty"`
	Files     []string   `json:"files,omitempty"`
	Worktree  *Isolation `json:"worktree,omitempty"`
}

// Done reports whether the task has stopped for good.
func (t Task) Done() bool {
	return t.Status != StatusQueued && t.Status != StatusRunning
}

// keepTasks bounds how many tasks a chat's roster remembers: enough to show
// the last few calls and their outcomes, not a history of every one.
const keepTasks = 24

// Roster knows every chat's subagents while the daemon runs. It is memory
// only, like the runs themselves: a restart ends them, and the channels keep
// the transcripts.
type Roster struct {
	bus      *eventbus.Bus
	mu       sync.Mutex
	byParent map[types.ID][]*Task
}

func NewRoster(bus *eventbus.Bus) *Roster {
	return &Roster{bus: bus, byParent: map[types.ID][]*Task{}}
}

func (r *Roster) add(parent types.ID, tasks []*Task) {
	r.mu.Lock()
	list := append(r.byParent[parent], tasks...)
	// forget the oldest finished ones first; a running task is never dropped
	for len(list) > keepTasks {
		cut := -1
		for i, t := range list {
			if t.Done() {
				cut = i
				break
			}
		}
		if cut < 0 {
			break
		}
		list = append(list[:cut], list[cut+1:]...)
	}
	r.byParent[parent] = list
	r.mu.Unlock()
	r.publish(parent)
}

// update changes a task under the lock and tells the app.
func (r *Roster) update(t *Task, change func(*Task)) {
	r.mu.Lock()
	change(t)
	parent := t.Parent
	r.mu.Unlock()
	r.publish(parent)
}

// List is a copy of a chat's tasks, newest call last.
func (r *Roster) List(parent types.ID) []Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Task, 0, len(r.byParent[parent]))
	for _, t := range r.byParent[parent] {
		c := *t
		if t.Worktree != nil {
			w := *t.Worktree
			c.Worktree = &w
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Batch != out[j].Batch {
			return false // batches stay in the order they were called
		}
		return out[i].Index < out[j].Index
	})
	return out
}

// Get finds a task by id, with the chat it belongs to.
func (r *Roster) Get(id string) (*Task, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, list := range r.byParent {
		for _, t := range list {
			if t.ID == id {
				return t, true
			}
		}
	}
	return nil, false
}

// Snapshot is Get's copy, safe to read without the lock.
func (r *Roster) Snapshot(id string) (Task, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, list := range r.byParent {
		for _, t := range list {
			if t.ID == id {
				c := *t
				if t.Worktree != nil {
					w := *t.Worktree
					c.Worktree = &w
				}
				return c, true
			}
		}
	}
	return Task{}, false
}

func (r *Roster) publish(parent types.ID) {
	if r.bus == nil {
		return
	}
	r.bus.Publish(types.Event{
		Type:    EventSubagents,
		Topic:   "session." + string(parent),
		Payload: map[string]any{"sessionId": string(parent), "tasks": r.List(parent)},
	})
}
