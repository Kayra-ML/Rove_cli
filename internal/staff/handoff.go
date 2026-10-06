package staff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/types"
)

// Handoff pairs two agents: when the first finishes a task, the second
// gets one — the handoff's instruction, with the first's report and the
// files it changed — and works on top of the first's changes, so the two
// reach the user as one piece of work to review. Drawn on the Session Map
// as a cable from one agent to the other.
//
// Chains are allowed (one agent hands to the next, and on) up to maxChain
// agents; an agent never gets a handoff from a chain it is already in, so
// two cannot pass work back and forth.
type Handoff struct {
	ID          types.ID `json:"id"`
	FromID      types.ID `json:"fromId"`
	ToID        types.ID `json:"toId"`
	Instruction string   `json:"instruction"`
	// DailyLimit caps the handed tasks a day; 0 takes DefaultDailyLimit.
	DailyLimit int       `json:"dailyLimit"`
	Enabled    bool      `json:"enabled"`
	Color      string    `json:"color,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

const maxChain = 3

func (h Handoff) limit() int {
	if h.DailyLimit <= 0 {
		return DefaultDailyLimit
	}
	return h.DailyLimit
}

func (e *Engine) Handoffs(ctx context.Context) ([]Handoff, error) {
	bodies, err := e.st.ListStaffHandoffs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Handoff, 0, len(bodies))
	for _, b := range bodies {
		var h Handoff
		if json.Unmarshal([]byte(b), &h) == nil {
			out = append(out, h)
		}
	}
	return out, nil
}

// SaveHandoff adds or changes a handoff; drawing the same pair again finds
// the one there is.
func (e *Engine) SaveHandoff(ctx context.Context, h Handoff) (Handoff, error) {
	if h.FromID == "" || h.ToID == "" || h.FromID == h.ToID {
		return h, errors.New("a handoff needs two different agents")
	}
	for _, pid := range []types.ID{h.FromID, h.ToID} {
		if _, err := e.st.GetAgentProfile(ctx, pid); err != nil {
			return h, errors.New("no such agent")
		}
	}
	if h.ID == "" {
		if list, err := e.Handoffs(ctx); err == nil {
			for _, x := range list {
				if x.FromID == h.FromID && x.ToID == h.ToID {
					return x, nil
				}
			}
		}
		h.ID, h.CreatedAt, h.Enabled = id.NewID(), time.Now().UTC(), true
	}
	h.Instruction = strings.TrimSpace(h.Instruction)
	if r := []rune(h.Instruction); len(r) > instructionRunes {
		h.Instruction = string(r[:instructionRunes])
	}
	if h.DailyLimit < 0 {
		h.DailyLimit = 0
	}
	if h.DailyLimit > maxDailyLimit {
		h.DailyLimit = maxDailyLimit
	}
	if !types.ValidCableColor(h.Color) {
		h.Color = ""
	}
	b, err := json.Marshal(h)
	if err != nil {
		return h, err
	}
	return h, e.st.PutStaffHandoff(ctx, h.ID, h.FromID, h.ToID, string(b), h.CreatedAt)
}

func (e *Engine) DeleteHandoff(ctx context.Context, hid types.ID) error {
	return e.st.DeleteStaffHandoff(ctx, hid)
}

// handOn passes a finished task along its agent's handoffs.
func (e *Engine) handOn(t Task) {
	if t.Status != StatusDone || t.From != "" {
		return // only finished work, and not a colleague's request
	}
	ctx := e.base
	list, err := e.Handoffs(ctx)
	if err != nil {
		return
	}
	chain := append(append([]types.ID(nil), t.Chain...), t.ProfileID)
	if len(chain) >= maxChain {
		return
	}
	for _, h := range list {
		if !h.Enabled || h.FromID != t.ProfileID || inChain(chain, h.ToID) || e.handoffBusy(ctx, h) {
			continue
		}
		var b strings.Builder
		instruction := h.Instruction
		if instruction == "" {
			instruction = "Take your colleague's finished work further in your own field: check it, complete it, fix what is wrong."
		}
		b.WriteString(instruction)
		fmt.Fprintf(&b, "\n\nYour colleague %s just finished: %s\n", t.ProfileName, t.Title)
		if t.Isolation != nil && len(t.Isolation.Files) > 0 {
			b.WriteString("Files they changed: " + strings.Join(t.Isolation.Files, ", ") + "\n")
			if t.Isolation.State == IsoReview {
				b.WriteString("You start from their changes; yours go on top, and the user reviews both together.\n")
			}
		}
		if t.Report != "" {
			b.WriteString("\nTheir report:\n" + clipTail(t.Report, reportFromChat) + "\n")
		}
		_, _ = e.Assign(ctx, AssignOpts{
			ProfileID: h.ToID, Brief: b.String(), WorkspaceID: t.WorkspaceID,
			HandoffID: h.ID, CarryFrom: t.ID, Chain: chain, Origin: t.ProfileName,
		})
	}
}

func inChain(chain []types.ID, pid types.ID) bool {
	for _, x := range chain {
		if x == pid {
			return true
		}
	}
	return false
}

// handoffBusy is true while a task of the handoff runs, or once it has used
// up today's cap.
func (e *Engine) handoffBusy(ctx context.Context, h Handoff) bool {
	tasks, err := e.Tasks(ctx, h.ToID, 0)
	if err != nil {
		return true
	}
	today := time.Now().Format("2006-01-02")
	n := 0
	for _, t := range tasks {
		if t.HandoffID != h.ID {
			continue
		}
		if t.Status == StatusRunning {
			return true
		}
		if t.CreatedAt.Local().Format("2006-01-02") == today {
			n++
		}
	}
	return n >= h.limit()
}
