package staff

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/types"
)

// Watch is an agent watching a chat, drawn on the Session Map as a cable
// from the chat to the agent: when a turn in that chat changes files, the
// agent gets a task — the watch's instruction, with what changed.
//
// What keeps it cheap and safe: a filter on the files, a daily cap, one task
// at a time per watch, and an off switch. A task's own chat never sets a
// watch off, so two agents cannot keep each other busy.
type Watch struct {
	ID          types.ID `json:"id"`
	SessionID   types.ID `json:"sessionId"`
	ProfileID   types.ID `json:"profileId"`
	Instruction string   `json:"instruction"`
	// Filter is comma-separated patterns ("*.go, src/*"); empty: any file.
	Filter string `json:"filter,omitempty"`
	// DailyLimit caps the tasks a day; 0 takes DefaultDailyLimit.
	DailyLimit int  `json:"dailyLimit"`
	Enabled    bool `json:"enabled"`
	// Color is the cable's colour on the map (types.CableColors).
	Color     string    `json:"color,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

const (
	DefaultDailyLimit = 10
	maxDailyLimit     = 200
	instructionRunes  = 2000
	reportFromChat    = 1200
)

func (w Watch) limit() int {
	if w.DailyLimit <= 0 {
		return DefaultDailyLimit
	}
	return w.DailyLimit
}

// Watches lists every watch.
func (e *Engine) Watches(ctx context.Context) ([]Watch, error) {
	return e.watches(ctx, "")
}

func (e *Engine) watches(ctx context.Context, sessionID types.ID) ([]Watch, error) {
	bodies, err := e.st.ListStaffWatches(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]Watch, 0, len(bodies))
	for _, b := range bodies {
		var w Watch
		if json.Unmarshal([]byte(b), &w) == nil {
			out = append(out, w)
		}
	}
	return out, nil
}

// SaveWatch adds a watch or changes one. A new watch is on, with the
// default cap and instruction.
func (e *Engine) SaveWatch(ctx context.Context, w Watch) (Watch, error) {
	if w.SessionID == "" || w.ProfileID == "" {
		return w, errors.New("a watch needs a chat and an agent")
	}
	if _, err := e.st.GetSession(ctx, w.SessionID); err != nil {
		return w, errors.New("no such chat")
	}
	if _, err := e.st.GetAgentProfile(ctx, w.ProfileID); err != nil {
		return w, errors.New("no such agent")
	}
	if w.ID == "" {
		// one watch per chat and agent: drawing the cable again finds it
		if list, err := e.watches(ctx, w.SessionID); err == nil {
			for _, x := range list {
				if x.ProfileID == w.ProfileID {
					return x, nil
				}
			}
		}
		w.ID, w.CreatedAt, w.Enabled = id.NewID(), time.Now().UTC(), true
	}
	w.Instruction = strings.TrimSpace(w.Instruction)
	if r := []rune(w.Instruction); len(r) > instructionRunes {
		w.Instruction = string(r[:instructionRunes])
	}
	w.Filter = strings.TrimSpace(w.Filter)
	if !types.ValidCableColor(w.Color) {
		w.Color = ""
	}
	if w.DailyLimit < 0 {
		w.DailyLimit = 0
	}
	if w.DailyLimit > maxDailyLimit {
		w.DailyLimit = maxDailyLimit
	}
	b, err := json.Marshal(w)
	if err != nil {
		return w, err
	}
	return w, e.st.PutStaffWatch(ctx, w.ID, w.SessionID, w.ProfileID, string(b), w.CreatedAt)
}

func (e *Engine) DeleteWatch(ctx context.Context, watchID types.ID) error {
	return e.st.DeleteStaffWatch(ctx, watchID)
}

// OnRunDone is the agent run hook: a turn that changed files sets off the
// watches on its chat.
func (e *Engine) OnRunDone(req agent.RunRequest, res agent.RunResult) {
	if req.SessionID == "" || len(res.FilesEdited) == 0 {
		return
	}
	ctx := context.Background()
	// a task's own chat never sets a watch off: no chains, no loops
	if _, err := e.st.StaffTaskBySession(ctx, req.SessionID); err == nil {
		return
	}
	ws, err := e.watches(ctx, req.SessionID)
	if err != nil || len(ws) == 0 {
		return
	}
	s, err := e.st.GetSession(ctx, req.SessionID)
	if err != nil {
		return
	}
	for _, w := range ws {
		if !w.Enabled {
			continue
		}
		files := matching(w.Filter, res.FilesEdited, req.Workspace)
		if len(files) == 0 || e.watchBusy(ctx, w) {
			continue
		}
		brief := watchBrief(w, s.Title, files, res.Assistant)
		_, _ = e.Assign(ctx, AssignOpts{ProfileID: w.ProfileID, Brief: brief, WorkspaceID: s.WorkspaceID, WatchID: w.ID, Origin: s.Title})
	}
}

// watchBusy is true while a task of the watch runs, or once it has used up
// today's cap.
func (e *Engine) watchBusy(ctx context.Context, w Watch) bool {
	tasks, err := e.Tasks(ctx, w.ProfileID, 0)
	if err != nil {
		return true
	}
	today := time.Now().Format("2006-01-02")
	n := 0
	for _, t := range tasks {
		if t.WatchID != w.ID {
			continue
		}
		if t.Status == StatusRunning {
			return true
		}
		if t.CreatedAt.Local().Format("2006-01-02") == today {
			n++
		}
	}
	return n >= w.limit()
}

// matching keeps the files a filter lets through, relative to the project
// where they are under it.
func matching(filter string, files []string, root string) []string {
	var pats []string
	for _, p := range strings.Split(filter, ",") {
		if p = strings.TrimSpace(p); p != "" {
			pats = append(pats, p)
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		rel := f
		if root != "" {
			rel = strings.TrimPrefix(strings.TrimPrefix(f, root), "/")
		}
		if seen[rel] {
			continue
		}
		ok := len(pats) == 0
		for _, p := range pats {
			if m, _ := path.Match(p, rel); m {
				ok = true
			} else if m, _ := path.Match(p, path.Base(rel)); m {
				ok = true
			} else if strings.HasSuffix(p, "/*") && strings.HasPrefix(rel, strings.TrimSuffix(p, "*")) {
				ok = true // "src/*" takes everything under src
			}
		}
		if ok {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	return out
}

func watchBrief(w Watch, chat string, files []string, said string) string {
	instruction := w.Instruction
	if instruction == "" {
		instruction = "Look over these changes in your field and fix or flag what needs it."
	}
	var b strings.Builder
	b.WriteString(instruction)
	b.WriteString("\n\nYou are watching the chat \"" + chat + "\". A turn there just changed:\n")
	for _, f := range files {
		b.WriteString("- " + f + "\n")
	}
	if said = strings.TrimSpace(said); said != "" {
		b.WriteString("\nWhat that chat's agent said about it:\n" + clipTail(said, reportFromChat) + "\n")
	}
	b.WriteString("\nRead the files themselves before you judge them.")
	return b.String()
}
