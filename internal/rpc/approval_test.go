package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/approval"
	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/permission"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// shellModel asks to run one command, then reports what came back.
type shellModel struct {
	mu   sync.Mutex
	runs int
	last string
}

func (*shellModel) Kind() types.ProviderKind { return types.ProviderFake }
func (*shellModel) Name() string             { return "shellm" }
func (m *shellModel) Complete(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	ch := make(chan provider.ChatDelta, 2)
	m.mu.Lock()
	m.runs++
	first := m.runs == 1
	if last := req.Messages[len(req.Messages)-1]; last.Role == "tool" {
		m.last = last.Content
	}
	m.mu.Unlock()
	if first {
		ch <- provider.ChatDelta{ToolCalls: []types.ToolCall{{ID: "c1", Name: "shell", ArgsJSON: `{"command":"echo merhaba"}`}}}
	} else {
		ch <- provider.ChatDelta{Content: "bitti"}
	}
	ch <- provider.ChatDelta{Done: true}
	close(ch)
	return ch, nil
}

func (m *shellModel) result() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
}

// A rule that says "ask" stops the run and puts the question to the user.
// Saying yes lets the command run; saying no refuses it without pretending
// something broke.
func TestPermissionIsAskedAndAnswered(t *testing.T) {
	for _, tc := range []struct {
		name  string
		allow bool
		want  string
	}{
		{"allowed", true, "merhaba"},
		{"denied", false, "the user said no"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, err := core.Open(config.Config{DataDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()
			app.Approvals.Wait = 5 * time.Second
			s := New(app)
			ctx := context.Background()
			m := &shellModel{}
			app.Router.Register("shellm", m)
			ag, _ := app.Agents.Upsert(ctx, types.Agent{Name: "a", Provider: "shellm", Model: "m"})
			call := func(method string, params, out any) string {
				raw, _ := json.Marshal(params)
				resp := s.Dispatch(ctx, protocol.Request{Method: method, Token: app.Token, Params: raw})
				if !resp.OK {
					return resp.Error
				}
				if out != nil {
					_ = json.Unmarshal(resp.Result, out)
				}
				return ""
			}
			var chat types.Session
			if e := call(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &chat); e != "" {
				t.Fatal(e)
			}

			done := make(chan struct{})
			go func() {
				defer close(done)
				call(protocol.MethodSessionSend, map[string]any{"sessionId": chat.ID, "workspace": t.TempDir(), "content": "komutu çalıştır"}, nil)
			}()

			// the run waits, and the question says what it is about
			var asks []approval.Request
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				asks = nil
				call(protocol.MethodPermissionAsks, map[string]any{}, &asks)
				if len(asks) == 1 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if len(asks) != 1 {
				t.Fatal("nothing was asked")
			}
			if asks[0].Tool != "shell" || asks[0].Detail != "echo merhaba" || asks[0].SessionID != chat.ID {
				t.Fatalf("question = %+v", asks[0])
			}

			if e := call(protocol.MethodPermissionAnswer, map[string]any{"id": asks[0].ID, "allow": tc.allow}, nil); e != "" {
				t.Fatal(e)
			}
			<-done
			if got := m.result(); !strings.Contains(got, tc.want) {
				t.Fatalf("tool result = %q, want %q", got, tc.want)
			}
			// the question is gone once answered
			var left []approval.Request
			call(protocol.MethodPermissionAsks, map[string]any{}, &left)
			if len(left) != 0 {
				t.Fatalf("still waiting: %+v", left)
			}
		})
	}
}

// "Always" writes the rule, so the next call goes straight through.
func TestAlwaysAllowIsRemembered(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	ctx := context.Background()
	if err := app.Approvals.Answer("nope", approval.Answer{Allow: true}); err == nil {
		t.Fatal("answering a question nobody asked should fail")
	}
	r := approval.Request{Action: types.PermShell, Tool: "shell"}
	if err := app.Approvals.Remember(ctx, r, true); err != nil {
		t.Fatal(err)
	}
	d, err := app.Perm.Evaluate(ctx, permission.Check{Action: types.PermShell, Target: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	if d != types.PermAllow {
		t.Fatalf("shell is still %q after remembering", d)
	}
}
