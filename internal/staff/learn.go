package staff

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/types"
)

// After every task an agent looks back at it and keeps what is worth
// knowing next time — a convention of the project, a pitfall, where things
// live, how the user likes it — as notes of its own, marked as learned. It
// is a short tool-less call; nothing is kept when nothing was learned. When
// the learned notes grow past a bound they are folded into fewer, so the
// notes every conversation carries stay short. Notes the user wrote are
// never touched; learned ones show on the board and can be deleted there.

// LearnedPrefix marks a note the agent kept itself.
const LearnedPrefix = "learned:"

const (
	lessonsPerTask = 2
	learnedMax     = 24 // past this the learned notes are folded
	learnedFolded  = 12
)

const reflectSystem = `You keep an AI agent's notes for one software project. From the finished task below, write at most 2 lessons worth knowing in later tasks on this project: a convention, a pitfall, where something lives, a command that works, how the user wants things. Not what this task did, and nothing already in the notes.
Each lesson on its own line starting with "- ", short and concrete, in the language the task was given in. If nothing is worth keeping, answer exactly NONE.`

const foldSystem = `Merge these notes an AI agent keeps about one project into at most 12 lines: keep every concrete fact, drop repeats and anything stale or contradicted by a later note. Each line starts with "- ". Answer with the lines only.`

// reflect is the look back after a task. It runs apart from the task and
// costs one short call.
func (e *Engine) reflect(t Task) {
	if e.Ask == nil || t.Status == StatusStopped {
		return
	}
	ctx, cancel := context.WithTimeout(e.base, 2*time.Minute)
	defer cancel()
	p, err := e.st.GetAgentProfile(ctx, t.ProfileID)
	if err != nil {
		return
	}
	notes, _ := e.Notes(ctx, p.ID)
	var b strings.Builder
	fmt.Fprintf(&b, "Agent: %s", p.Name)
	if p.Title != "" {
		b.WriteString(" — " + p.Title)
	}
	b.WriteString("\n\nNotes it keeps already:\n")
	if len(notes) == 0 {
		b.WriteString("(none)\n")
	}
	for _, n := range notes {
		b.WriteString("- " + n.Content + "\n")
	}
	b.WriteString("\nTask:\n" + clip(t.Brief, 2000) + "\n\nHow it ended: " + string(t.Status))
	if t.Error != "" {
		b.WriteString(" — " + clip(t.Error, 300))
	}
	if t.Isolation != nil && len(t.Isolation.Files) > 0 {
		b.WriteString("\nFiles it changed: " + strings.Join(t.Isolation.Files, ", "))
	}
	b.WriteString("\n\nIts report:\n" + clip(t.Report, 1500))
	agentID, err := e.askAgent(ctx)
	if err != nil {
		return
	}
	out, err := e.Ask(ctx, agentID, reflectSystem, b.String())
	if err != nil {
		return
	}
	lessons := parseLines(out, lessonsPerTask)
	for _, l := range lessons {
		_, _ = e.addLearned(ctx, p.ID, l)
	}
	if len(lessons) > 0 {
		e.fold(ctx, p.ID, agentID)
		e.notesChanged(p.ID)
	}
}

func (e *Engine) askAgent(ctx context.Context) (types.ID, error) {
	if e.DefaultAgent == nil {
		return "", fmt.Errorf("no agent to ask")
	}
	return e.DefaultAgent(ctx)
}

func (e *Engine) addLearned(ctx context.Context, profileID types.ID, text string) (types.MemoryEntry, error) {
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > noteRunes {
		text = string(r[:noteRunes])
	}
	n := types.MemoryEntry{ID: id.NewID(), Scope: types.MemProfile, ScopeID: profileID, Key: LearnedPrefix + keyOf(text), Content: text, CreatedAt: time.Now().UTC()}
	return n, e.st.PutMemory(ctx, n)
}

// fold merges the learned notes once there are too many.
func (e *Engine) fold(ctx context.Context, profileID, agentID types.ID) {
	notes, err := e.Notes(ctx, profileID)
	if err != nil {
		return
	}
	var learned []types.MemoryEntry
	for _, n := range notes {
		if strings.HasPrefix(n.Key, LearnedPrefix) {
			learned = append(learned, n)
		}
	}
	if len(learned) <= learnedMax {
		return
	}
	var b strings.Builder
	for _, n := range learned {
		b.WriteString("- " + n.Content + "\n")
	}
	out, err := e.Ask(ctx, agentID, foldSystem, b.String())
	if err != nil {
		return
	}
	merged := parseLines(out, learnedFolded)
	if len(merged) == 0 {
		return
	}
	for _, n := range learned {
		_ = e.st.DeleteMemory(ctx, n.ID)
	}
	for _, l := range merged {
		_, _ = e.addLearned(ctx, profileID, l)
	}
}

// parseLines reads "- " lines, at most n; NONE is none.
func parseLines(out string, n int) []string {
	out = strings.TrimSpace(out)
	if out == "" || strings.EqualFold(strings.Trim(out, ". "), "NONE") {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "- ") && !strings.HasPrefix(l, "* ") {
			continue
		}
		l = strings.TrimSpace(l[2:])
		if l == "" || strings.EqualFold(l, "NONE") {
			continue
		}
		lines = append(lines, l)
		if len(lines) >= n {
			break
		}
	}
	return lines
}

func (e *Engine) notesChanged(profileID types.ID) {
	if e.bus != nil {
		e.bus.Publish(types.Event{Type: EventUpdated, Topic: "staff", Timestamp: time.Now().UTC(), Payload: map[string]any{
			"profileId": string(profileID), "notes": true,
		}})
	}
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
