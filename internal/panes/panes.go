// Package panes runs a session's terminals as one team: the terminal mode of
// a session splits into several terminals (the session itself is T1, the
// others are its children), each with its own agent and role. They see a
// one-line-each board of what the others do, a file one of them is editing
// is closed to the rest until its run ends, and any of them can hand work
// to another.
package panes

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

// MaxTerminals a session can have (the deck's largest split).
const MaxTerminals = 8

// maxHop caps chains of agents handing work on (T1 → T2 → T3), so two
// terminals cannot keep each other busy forever.
const maxHop = 2

// Board budgets (characters): what one terminal's line may carry.
const (
	taskClip  = 90
	fileLimit = 5
)

// Runner starts one agent run; *agent.Runtime.Run fits.
type Runner func(ctx context.Context, req agent.RunRequest) (agent.RunResult, error)

// Hub knows the terminals of every session.
type Hub struct {
	st *store.Store
	sm *session.Manager

	Run     Runner
	Running func(types.ID) bool
	// RoleOf is a terminal's role: its character's id and name ("" for none).
	RoleOf func(ctx context.Context, sessionID types.ID) (id, name string)
	// WorkspacePath resolves a workspace id to its folder.
	WorkspacePath func(ctx context.Context, id types.ID) string

	mu     sync.Mutex
	claims map[string]claim
	hops   map[types.ID]int
}

type claim struct {
	owner types.ID
	label string
}

func New(st *store.Store, sm *session.Manager) *Hub {
	return &Hub{st: st, sm: sm, claims: map[string]claim{}, hops: map[types.ID]int{}}
}

// Pane is one terminal, as the board and /panes show it.
type Pane struct {
	N     int      `json:"n"`
	ID    types.ID `json:"id"`
	Label string   `json:"label"` // "T2"
	Title string   `json:"title"`
	Role  string   `json:"role,omitempty"`
	// CharacterID lets the app show the role in the user's language.
	CharacterID string   `json:"characterId,omitempty"`
	Running     bool     `json:"running"`
	Task        string   `json:"task,omitempty"`    // the last thing it was asked
	Editing     []string `json:"editing,omitempty"` // files it holds now
	Changed     []string `json:"changed,omitempty"` // files its last run changed
}

// group is a session's terminals: the session (T1), then its terminal
// children in the order they were opened.
func (h *Hub) group(ctx context.Context, sessionID types.ID) (types.Session, []types.Session, error) {
	s, err := h.st.GetSession(ctx, sessionID)
	if err != nil {
		return types.Session{}, nil, err
	}
	parent := s
	if s.ParentID != "" {
		if s.Space != types.SpaceTerminal {
			return s, []types.Session{s}, nil // a team member channel: no terminals
		}
		if parent, err = h.st.GetSession(ctx, s.ParentID); err != nil {
			return types.Session{}, nil, err
		}
	}
	kids, err := h.sm.Children(ctx, parent.ID)
	if err != nil {
		return types.Session{}, nil, err
	}
	members := []types.Session{parent}
	for _, k := range kids {
		if k.Space == types.SpaceTerminal {
			members = append(members, k)
		}
	}
	return parent, members, nil
}

// NewPane opens another terminal in a session.
func (h *Hub) NewPane(ctx context.Context, parentID types.ID) (types.Session, error) {
	parent, members, err := h.group(ctx, parentID)
	if err != nil {
		return types.Session{}, err
	}
	if parent.ID != parentID {
		return types.Session{}, errors.New("terminals belong to a session, not to another terminal")
	}
	if len(members) >= MaxTerminals {
		return types.Session{}, fmt.Errorf("a session has at most %d terminals", MaxTerminals)
	}
	return h.sm.CreateChildIn(ctx, parent, types.SpaceTerminal, fmt.Sprintf("Terminal %d", len(members)+1))
}

// Panes lists a session's terminals.
func (h *Hub) Panes(ctx context.Context, sessionID types.ID) ([]Pane, error) {
	_, members, err := h.group(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	editing := map[types.ID][]string{}
	for key, c := range h.claims {
		_, path, _ := strings.Cut(key, "\x00")
		editing[c.owner] = append(editing[c.owner], filepath.Base(path))
	}
	h.mu.Unlock()
	out := make([]Pane, 0, len(members))
	for i, m := range members {
		p := Pane{N: i + 1, ID: m.ID, Label: "T" + strconv.Itoa(i+1), Title: m.Title}
		if h.RoleOf != nil {
			p.CharacterID, p.Role = h.RoleOf(ctx, m.ID)
		}
		if h.Running != nil {
			p.Running = h.Running(m.ID)
		}
		p.Task = lastTask(ctx, h.st, m.ID)
		p.Editing = limit(editing[m.ID])
		if paths, err := h.st.TurnEditPaths(ctx, m.ID, ""); err == nil {
			p.Changed = limit(paths)
		}
		out = append(out, p)
	}
	return out, nil
}

func limit(list []string) []string {
	if len(list) > fileLimit {
		return append(list[:fileLimit:fileLimit], fmt.Sprintf("+%d", len(list)-fileLimit))
	}
	return list
}

// lastTask is the first line of the last thing a terminal was asked.
func lastTask(ctx context.Context, st *store.Store, id types.ID) string {
	content, err := st.LastUserMessage(ctx, id)
	if err != nil {
		return ""
	}
	t := strings.TrimSpace(content)
	// attached files and search results are not the task
	if j := strings.LastIndex(t, "</file>"); j >= 0 {
		t = strings.TrimSpace(t[j+len("</file>"):])
	}
	if j := strings.IndexByte(t, '\n'); j >= 0 {
		t = t[:j]
	}
	if r := []rune(t); len(r) > taskClip {
		t = string(r[:taskClip]) + "…"
	}
	return t
}

func (p Pane) name() string {
	if p.Role != "" {
		return p.Label + " (" + p.Role + ")"
	}
	return p.Label
}

// Board is what one terminal is told about the others, at the end of its
// system prompt: a line each. Empty when the session has one terminal.
func (h *Hub) Board(ctx context.Context, sessionID types.ID) string {
	panes, err := h.Panes(ctx, sessionID)
	if err != nil || len(panes) < 2 {
		return ""
	}
	var me Pane
	for _, p := range panes {
		if p.ID == sessionID {
			me = p
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Terminals of this session\nYou are %s, one of %d terminals working on the same project at the same time. ", me.name(), len(panes))
	b.WriteString("Stay on your own part. A file another terminal is editing is closed to you until it finishes; pick other work or ask it with terminal_send.\n")
	for _, p := range panes {
		if p.ID == sessionID {
			continue
		}
		state := "idle"
		if p.Running {
			state = "working"
		}
		line := fmt.Sprintf("- %s: %s", p.name(), state)
		if p.Task != "" {
			line += " — " + p.Task
		}
		if len(p.Editing) > 0 {
			line += " — editing " + strings.Join(p.Editing, ", ")
		} else if len(p.Changed) > 0 {
			line += " — changed " + strings.Join(p.Changed, ", ")
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// Resolve finds a terminal of the session by "2", "T2", its role or its
// title (case-insensitive).
func (h *Hub) Resolve(ctx context.Context, sessionID types.ID, ref string) (Pane, error) {
	panes, err := h.Panes(ctx, sessionID)
	if err != nil {
		return Pane{}, err
	}
	r := strings.ToLower(strings.TrimSpace(ref))
	r = strings.TrimPrefix(r, "@")
	if n, err := strconv.Atoi(strings.TrimPrefix(r, "t")); err == nil {
		for _, p := range panes {
			if p.N == n {
				return p, nil
			}
		}
		return Pane{}, fmt.Errorf("no terminal T%d (this session has %d)", n, len(panes))
	}
	for _, p := range panes {
		if r != "" && (strings.Contains(strings.ToLower(p.Role), r) || strings.Contains(strings.ToLower(p.Title), r)) {
			return p, nil
		}
	}
	return Pane{}, fmt.Errorf("no terminal matches %q", ref)
}

// Send gives a terminal a message to work on, from another terminal of the
// same session (byAgent: its agent, through terminal_send; else the user).
// It does not wait for the work; a busy terminal is not interrupted.
func (h *Hub) Send(ctx context.Context, from types.ID, ref, message string, byAgent bool) (Pane, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return Pane{}, errors.New("nothing to send")
	}
	to, err := h.Resolve(ctx, from, ref)
	if err != nil {
		return Pane{}, err
	}
	if to.ID == from {
		return Pane{}, errors.New("that is this terminal")
	}
	if to.Running {
		return to, fmt.Errorf("%s is busy; send it when it is done", to.name())
	}
	hop := 0
	if byAgent {
		h.mu.Lock()
		hop = h.hops[from] + 1
		h.mu.Unlock()
		if hop > maxHop {
			return to, errors.New("too many hand-offs in a row; finish this part yourself")
		}
		fromPane, _ := h.paneOf(ctx, from)
		message = "[from " + fromPane.name() + "] " + message
	}
	target, err := h.st.GetSession(ctx, to.ID)
	if err != nil {
		return to, err
	}
	ws := ""
	if h.WorkspacePath != nil && target.WorkspaceID != "" {
		ws = h.WorkspacePath(ctx, target.WorkspaceID)
	}
	h.mu.Lock()
	h.hops[to.ID] = hop
	h.mu.Unlock()
	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.hops, to.ID)
			h.mu.Unlock()
		}()
		_, _ = h.Run(context.Background(), agent.RunRequest{
			AgentID: target.AgentID, SessionID: target.ID, WorkspaceID: target.WorkspaceID, Workspace: ws,
			UserMessage: message,
		})
	}()
	return to, nil
}

func (h *Hub) paneOf(ctx context.Context, id types.ID) (Pane, error) {
	panes, err := h.Panes(ctx, id)
	if err != nil {
		return Pane{}, err
	}
	for _, p := range panes {
		if p.ID == id {
			return p, nil
		}
	}
	return Pane{}, errors.New("not a terminal")
}

// ── file claims (agent.Claims) ────────────────────────────────────────────────

// crew is everyone who may be writing the same workspace at the same time:
// the session and every child of it, terminals and team-member channels
// alike. It is wider than group, which is only about terminals — a team's
// members write the same files as each other, so they need the same guard.
func (h *Hub) crew(ctx context.Context, sessionID types.ID) (types.Session, int, error) {
	s, err := h.st.GetSession(ctx, sessionID)
	if err != nil {
		return types.Session{}, 0, err
	}
	parent := s
	if s.ParentID != "" {
		if parent, err = h.st.GetSession(ctx, s.ParentID); err != nil {
			return types.Session{}, 0, err
		}
	}
	kids, err := h.sm.Children(ctx, parent.ID)
	if err != nil {
		return types.Session{}, 0, err
	}
	return parent, 1 + len(kids), nil
}

// label names whoever holds a file, for the message the other one gets:
// "T2 (backend)" for a terminal, the character's name for a team member.
// The terminal name is only right when there really is a deck — on its own
// a member channel still answers to "T1", which would name the wrong thing.
func (h *Hub) label(ctx context.Context, sessionID types.ID) string {
	if _, deck, err := h.group(ctx, sessionID); err == nil && len(deck) > 1 {
		if p, err := h.paneOf(ctx, sessionID); err == nil {
			return p.name()
		}
	}
	if h.RoleOf != nil {
		if _, name := h.RoleOf(ctx, sessionID); name != "" {
			return name
		}
	}
	if s, err := h.st.GetSession(ctx, sessionID); err == nil && s.Title != "" {
		return s.Title
	}
	return "another agent"
}

// Claim lets one agent write a file unless another of the same session holds
// it and is still working.
func (h *Hub) Claim(ctx context.Context, sessionID types.ID, workspace, path string) string {
	parent, n, err := h.crew(ctx, sessionID)
	if err != nil || n < 2 {
		return "" // working alone: nothing to guard
	}
	abs := path
	if !filepath.IsAbs(abs) && workspace != "" {
		abs = filepath.Join(workspace, path)
	}
	key := string(parent.ID) + "\x00" + filepath.Clean(abs)
	h.mu.Lock()
	c, held := h.claims[key]
	h.mu.Unlock()
	if held && c.owner != sessionID && h.Running != nil && h.Running(c.owner) {
		return c.label
	}
	label := h.label(ctx, sessionID)
	h.mu.Lock()
	h.claims[key] = claim{owner: sessionID, label: label}
	h.mu.Unlock()
	return ""
}

// Release frees the files a terminal held; its run ended.
func (h *Hub) Release(sessionID types.ID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, c := range h.claims {
		if c.owner == sessionID {
			delete(h.claims, k)
		}
	}
}
