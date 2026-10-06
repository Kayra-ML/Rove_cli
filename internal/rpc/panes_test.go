package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/panes"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// paneModel plays each terminal by what it is asked: "T1 yaz" writes
// app.go and then waits (holding the file) until released; "T2 yaz" tries
// the same file; anything else answers. It keeps what each run was told.
type paneModel struct {
	mu      sync.Mutex
	release chan struct{}
	systems map[string]string // last user message → system prompt
	tools   map[string][]string
}

func (*paneModel) Kind() types.ProviderKind { return types.ProviderFake }
func (*paneModel) Name() string             { return "panem" }
func (m *paneModel) Complete(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	var user string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			user = req.Messages[i].Content
			break
		}
	}
	afterTool := req.Messages[len(req.Messages)-1].Role == "tool"
	m.mu.Lock()
	m.systems[user] = req.Messages[0].Content
	var names []string
	for _, t := range req.Tools {
		names = append(names, t.Name)
	}
	m.tools[user] = names
	m.mu.Unlock()
	ch := make(chan provider.ChatDelta, 2)
	switch {
	case strings.Contains(user, "T1 yaz") && !afterTool:
		ch <- provider.ChatDelta{ToolCalls: []types.ToolCall{{ID: "a", Name: "write_file", ArgsJSON: `{"path":"app.go","content":"package app // T1\n"}`}}}
	case strings.Contains(user, "T1 yaz"):
		select { // T1 keeps working (and holding app.go) until released
		case <-m.release:
		case <-ctx.Done():
		}
		ch <- provider.ChatDelta{Content: "T1 bitti"}
	case strings.Contains(user, "T2 yaz") && !afterTool:
		ch <- provider.ChatDelta{ToolCalls: []types.ToolCall{{ID: "b", Name: "write_file", ArgsJSON: `{"path":"app.go","content":"package app // T2\n"}`}}}
	default:
		ch <- provider.ChatDelta{Content: "tamam"}
	}
	ch <- provider.ChatDelta{Done: true}
	close(ch)
	return ch, nil
}

func (m *paneModel) system(user string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.systems[user]
}

func TestTerminalsWorkAsOneSession(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	m := &paneModel{release: make(chan struct{}), systems: map[string]string{}, tools: map[string][]string{}}
	app.Router.Register("panem", m)
	ag, _ := app.Agents.Upsert(ctx, types.Agent{Name: "p", Provider: "panem", Model: "m"})
	ws := t.TempDir()
	call := func(method string, params, out any) string {
		t.Helper()
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
	must := func(e string) {
		t.Helper()
		if e != "" {
			t.Fatal(e)
		}
	}

	var t1 types.Session
	must(call(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &t1))
	var t2 types.Session
	must(call(protocol.MethodTerminalNewPane, map[string]any{"parentId": t1.ID}, &t2))
	if t2.ParentID != t1.ID || t2.Space != types.SpaceTerminal {
		t.Fatalf("pane = %+v", t2)
	}
	// terminals are a session's subtitles: not chats of their own
	var list []types.Session
	must(call(protocol.MethodSessionList, map[string]any{}, &list))
	if len(list) != 1 {
		t.Fatalf("terminals listed as chats: %+v", list)
	}
	// T2 gets a role
	must(call(protocol.MethodPersonaSet, map[string]any{"sessionId": t2.ID, "characterId": "frontend"}, nil))

	// T1 starts writing app.go and keeps working
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		call(protocol.MethodSessionSend, map[string]any{"sessionId": t1.ID, "workspace": ws, "content": "T1 yaz"}, nil)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(filepath.Join(ws, "app.go")); strings.Contains(string(b), "T1") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// T2 tries the same file: refused, and T2 knows why
	must(call(protocol.MethodSessionSend, map[string]any{"sessionId": t2.ID, "workspace": ws, "content": "T2 yaz"}, nil))
	if b, _ := os.ReadFile(filepath.Join(ws, "app.go")); !strings.Contains(string(b), "T1") {
		t.Fatalf("T2 overwrote T1's file: %s", b)
	}
	var hist []types.Message
	call(protocol.MethodSessionHistory, map[string]any{"sessionId": t2.ID}, &hist)
	refused := false
	for _, msg := range hist {
		if msg.ToolResult != nil && strings.Contains(msg.ToolResult.Content, "T1 is editing this file right now") {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("T2 was not told the file is taken: %+v", hist)
	}
	// T2 saw the board: T1 working on its task, holding app.go
	sys := m.system("T2 yaz")
	if !strings.Contains(sys, "You are T2 (Frontend Expert), one of 2 terminals") || !strings.Contains(sys, "- T1: working — T1 yaz — editing app.go") {
		t.Fatalf("T2's board:\n%s", sys)
	}
	// only terminals of a multi-terminal session get the hand-off tool
	m.mu.Lock()
	hasSend := strings.Contains(strings.Join(m.tools["T2 yaz"], ","), panes.SendName)
	m.mu.Unlock()
	if !hasSend {
		t.Fatal("terminal_send not offered to a terminal")
	}
	var ps []panes.Pane
	must(call(protocol.MethodTerminalPanes, map[string]any{"sessionId": t2.ID}, &ps))
	if len(ps) != 2 || !ps[0].Running || ps[1].Role != "Frontend Expert" || ps[1].Label != "T2" {
		t.Fatalf("panes = %+v", ps)
	}
	// a busy terminal is not interrupted by another's message
	if e := call(protocol.MethodTerminalSendPane, map[string]any{"from": t2.ID, "to": "1", "message": "dur"}, nil); !strings.Contains(e, "busy") {
		t.Fatalf("send to busy = %q", e)
	}
	close(m.release)
	<-done1
	// T1 hands T2 a task, by its role
	var to panes.Pane
	must(call(protocol.MethodTerminalSendPane, map[string]any{"from": t1.ID, "to": "frontend", "message": "testleri yaz"}, &to))
	if to.Label != "T2" {
		t.Fatalf("resolved %+v", to)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && m.system("testleri yaz") == "" {
		time.Sleep(20 * time.Millisecond)
	}
	if m.system("testleri yaz") == "" {
		t.Fatal("T2 never got the task")
	}

	// the deck's layout is kept with the session
	layout := map[string]any{"split": 4, "panes": []map[string]any{{"kind": "chat", "sessionId": t1.ID}, {"kind": "chat", "sessionId": t2.ID}, {"kind": "shell", "termId": "x"}}}
	must(call(protocol.MethodTerminalLayoutSet, map[string]any{"sessionId": t1.ID, "layout": layout}, nil))
	var got map[string]any
	must(call(protocol.MethodTerminalLayoutGet, map[string]any{"sessionId": t1.ID}, &got))
	if got["split"].(float64) != 4 || len(got["panes"].([]any)) != 3 {
		t.Fatalf("layout = %+v", got)
	}
	// a single-terminal chat gets no board
	var solo types.Session
	must(call(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &solo))
	must(call(protocol.MethodSessionSend, map[string]any{"sessionId": solo.ID, "content": "yalnız"}, nil))
	if strings.Contains(m.system("yalnız"), "Terminals of this session") {
		t.Fatal("a lone chat got a terminal board")
	}
	// deleting the session takes its terminals along (after T2's run ends)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && app.Agents.Running(t2.ID) {
		time.Sleep(20 * time.Millisecond)
	}
	must(call(protocol.MethodSessionDelete, map[string]any{"id": t1.ID}, nil))
	if _, err := app.Store.GetSession(ctx, t2.ID); err == nil {
		t.Fatal("terminal left behind")
	}
}
