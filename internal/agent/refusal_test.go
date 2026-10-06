package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Kayra-ML/rove/internal/permission"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

// asker calls shell once, then answers.
type asker struct{ calls int }

func (*asker) Kind() types.ProviderKind { return types.ProviderFake }
func (*asker) Name() string             { return "asker" }
func (a *asker) Complete(_ context.Context, _ provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	ch := make(chan provider.ChatDelta, 2)
	a.calls++
	if a.calls == 1 {
		ch <- provider.ChatDelta{ToolCalls: []types.ToolCall{{ID: "t1", Name: "shell", ArgsJSON: `{"command":"echo hi"}`}}}
	} else {
		ch <- provider.ChatDelta{Content: "olmadı"}
	}
	ch <- provider.ChatDelta{Done: true}
	close(ch)
	return ch, nil
}

// A rule that says "ask" refuses the call. That is a decision, not a
// failure: it is marked as a refusal, and its text tells the model to stop
// retrying and what the user can do.
func TestPermissionRefusalIsMarkedAsOne(t *testing.T) {
	rt, _, _, sess := ctxRig(t, nil)
	ctx := context.Background()
	tools := tool.New(permission.New(rt.store))
	tools.Register(tool.Shell{})
	rt.tools = tools
	m := &asker{}
	rt.router.Register("asker", m)
	ag, _ := rt.Upsert(ctx, types.Agent{Name: "s", Provider: "asker", Model: "m"})
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "çalıştır"}); err != nil {
		t.Fatal(err)
	}
	msgs, _ := sess.History(ctx, s.ID)
	var got *types.ToolResult
	for _, msg := range msgs {
		if msg.ToolResult != nil {
			got = msg.ToolResult
		}
	}
	if got == nil {
		t.Fatal("no tool result")
	}
	if got.Kind != types.ToolRefused || !got.IsError {
		t.Fatalf("result = %+v", got)
	}
	for _, want := range []string{"not allowed", "Do not try it again", "Settings"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("refusal does not say %q: %s", want, got.Content)
		}
	}
	// it survives the round trip to the app
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"kind":"refused"`) {
		t.Fatalf("kind is not sent: %s", b)
	}
}

// A tool the session turned off is a refusal too, not a crash.
func TestDisabledToolIsARefusal(t *testing.T) {
	rt, _, _, sess := ctxRig(t, func(context.Context, types.ID) Persona {
		return Persona{Allow: func(string) bool { return false }}
	})
	ctx := context.Background()
	tools := tool.New(nil)
	tools.Register(tool.Shell{})
	rt.tools = tools
	m := &asker{}
	rt.router.Register("asker", m)
	ag, _ := rt.Upsert(ctx, types.Agent{Name: "s", Provider: "asker", Model: "m"})
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "çalıştır"}); err != nil {
		t.Fatal(err)
	}
	msgs, _ := sess.History(ctx, s.ID)
	for _, msg := range msgs {
		if msg.ToolResult != nil && msg.ToolResult.Kind != types.ToolRefused {
			t.Fatalf("result = %+v", msg.ToolResult)
		}
	}
}

// The prompt says where the agent is, so it does not invent a home folder.
func TestPromptGivesRealPaths(t *testing.T) {
	rt, rec, ag, sess := ctxRig(t, nil)
	ctx := context.Background()
	ws := t.TempDir()
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, Workspace: ws, UserMessage: "x"}); err != nil {
		t.Fatal(err)
	}
	sys := rec.last().Messages[0].Content
	home, _ := os.UserHomeDir()
	for _, want := range []string{"## Paths", "Workspace: " + ws, "Home: " + home, "Never invent a path"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, sys)
		}
	}
}
