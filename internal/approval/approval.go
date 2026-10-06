// Package approval asks the user before a tool call that a rule leaves open
// ("ask"). The agent's run blocks on the question, the app shows it, and the
// answer either lets the call through or refuses it. Nothing runs while the
// question stands, and an unanswered question refuses itself, so a chat left
// alone cannot sit waiting forever.
package approval

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/types"
)

// Asked is the event the app listens for.
const Asked types.EventType = "permission.ask"

// Answered tells the app a question is settled — by the user in another
// window, or by the wait running out — so it can take the dialog away.
const Answered types.EventType = "permission.answered"

// DefaultWait is how long a question stands before it refuses itself.
const DefaultWait = 5 * time.Minute

// Request is one question: what the agent wants to do, and where.
type Request struct {
	ID        types.ID               `json:"id"`
	SessionID types.ID               `json:"sessionId,omitempty"`
	Action    types.PermissionAction `json:"action"`
	Tool      string                 `json:"tool"`
	// Detail is the part the answer turns on: the command, the path, the URL.
	Detail  string    `json:"detail,omitempty"`
	AskedAt time.Time `json:"askedAt"`
}

// Answer is what the user said.
type Answer struct {
	Allow bool `json:"allow"`
	// Remember keeps the answer as a rule, so it is not asked again.
	Remember bool `json:"remember"`
}

// ErrUnknown is an answer to a question that is no longer waiting.
var ErrUnknown = errors.New("that question is not waiting for an answer")

// Gate holds the questions waiting for an answer.
type Gate struct {
	bus *eventbus.Bus
	// Wait is how long a question stands; 0 means DefaultWait.
	Wait time.Duration
	// Remember is called when the user says "always": it writes the rule.
	Remember func(ctx context.Context, r Request, allow bool) error

	mu      sync.Mutex
	waiting map[types.ID]entry
}

type entry struct {
	req Request
	ch  chan Answer
}

func New(bus *eventbus.Bus) *Gate {
	return &Gate{bus: bus, waiting: map[types.ID]entry{}}
}

// Ask puts the question to the user and waits. It reports whether the call
// may run; a question nobody answers, or a run that is cancelled first,
// does not.
func (g *Gate) Ask(ctx context.Context, r Request) (bool, error) {
	r.ID, r.AskedAt = id.NewID(), time.Now().UTC()
	ch := make(chan Answer, 1)
	g.mu.Lock()
	g.waiting[r.ID] = entry{req: r, ch: ch}
	g.mu.Unlock()
	defer g.drop(r.ID)

	g.publish(Asked, r, nil)
	wait := g.Wait
	if wait <= 0 {
		wait = DefaultWait
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case a := <-ch:
		if a.Remember && g.Remember != nil {
			_ = g.Remember(ctx, r, a.Allow)
		}
		return a.Allow, nil
	case <-timer.C:
		g.publish(Answered, r, boolp(false))
		return false, nil
	case <-ctx.Done():
		g.publish(Answered, r, boolp(false))
		return false, ctx.Err()
	}
}

// Answer settles a waiting question.
func (g *Gate) Answer(reqID types.ID, a Answer) error {
	g.mu.Lock()
	e, ok := g.waiting[reqID]
	g.mu.Unlock()
	if !ok {
		return ErrUnknown
	}
	select {
	case e.ch <- a:
	default: // already answered
	}
	g.publish(Answered, e.req, &a.Allow)
	return nil
}

// Pending is the questions still waiting, oldest first. An app that was
// closed or reloaded uses it to catch up.
func (g *Gate) Pending() []Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Request, 0, len(g.waiting))
	for _, e := range g.waiting {
		out = append(out, e.req)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].AskedAt.Before(out[j-1].AskedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (g *Gate) drop(reqID types.ID) {
	g.mu.Lock()
	delete(g.waiting, reqID)
	g.mu.Unlock()
}

func (g *Gate) publish(t types.EventType, r Request, allow *bool) {
	if g.bus == nil {
		return
	}
	p := map[string]any{
		"id": string(r.ID), "sessionId": string(r.SessionID),
		"action": string(r.Action), "tool": r.Tool, "detail": r.Detail,
	}
	if allow != nil {
		p["allow"] = *allow
	}
	g.bus.Publish(types.Event{Type: t, Topic: "permission", Payload: p})
}

func boolp(b bool) *bool { return &b }
