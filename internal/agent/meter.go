package agent

import (
	"context"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

// EventUsage fires once a model call is in the ledger.
const EventUsage types.EventType = "usage.recorded"

// metered is one model call on its way through: what was sent, counted by
// Rove, and what comes back — the reply as Rove sees it, and whatever usage
// the provider reports alongside. done files it in the usage ledger, so the
// app can show day by day what was spent, by its own count and by the
// provider's, side by side.
type metered struct {
	rt       *Runtime
	kind     string
	session  types.ID
	provider string
	model    string
	m        provider.Measure
	usage    provider.Usage
	reported bool
	start    time.Time
	text     strings.Builder
	calls    []types.ToolCall
}

func (rt *Runtime) meter(kind string, sessionID types.ID, c provider.Completer, model string, req provider.ChatRequest) *metered {
	name := ""
	if c != nil {
		name = c.Name()
	}
	return &metered{rt: rt, kind: kind, session: sessionID, provider: name, model: model, m: provider.MeasureRequest(req), start: time.Now()}
}

func (x *metered) see(d provider.ChatDelta) {
	if d.Usage != nil {
		if x.rt.router != nil {
			x.rt.router.Meter.Add(*d.Usage)
		}
		x.usage.PromptTokens += d.Usage.PromptTokens
		x.usage.CompletionTokens += d.Usage.CompletionTokens
		x.usage.CachedTokens += d.Usage.CachedTokens
		x.reported = x.reported || d.Usage.PromptTokens > 0 || d.Usage.CompletionTokens > 0
	}
	x.text.WriteString(d.Content)
	x.calls = append(x.calls, d.ToolCalls...)
}

// done records the call. It is written apart from the run's context: a
// call that was stopped still spent what it spent.
func (x *metered) done() {
	x.m.AddReply(x.text.String(), x.calls)
	if x.rt.store == nil {
		return
	}
	e := store.UsageLedgerEntry{
		ID: string(id.NewID()), SessionID: string(x.session), Kind: x.kind, Provider: x.provider, Model: x.model,
		PromptTokens: x.usage.PromptTokens, CompletionTokens: x.usage.CompletionTokens, CachedTokens: x.usage.CachedTokens,
		Reported: x.reported, SentChars: x.m.SentChars, SentTokens: x.m.SentTokens, RecvChars: x.m.RecvChars, RecvTokens: x.m.RecvTokens,
		DurationMS: time.Since(x.start).Milliseconds(), CreatedAt: time.Now().UTC(),
	}
	if err := x.rt.store.RecordUsage(context.Background(), e); err != nil {
		return
	}
	if x.rt.bus != nil {
		x.rt.bus.Publish(types.Event{Type: EventUsage, Topic: "usage", Timestamp: e.CreatedAt, Payload: map[string]any{
			"sessionId": e.SessionID, "provider": e.Provider, "model": e.Model,
		}})
	}
}
