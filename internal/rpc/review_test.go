package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// editModel changes one file and creates another, then says it is done.
type editModel struct {
	mu sync.Mutex
	n  int
}

func (*editModel) Kind() types.ProviderKind { return types.ProviderFake }
func (*editModel) Name() string             { return "editm" }
func (m *editModel) Complete(context.Context, provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	m.mu.Lock()
	m.n++
	n := m.n
	m.mu.Unlock()
	ch := make(chan provider.ChatDelta, 2)
	if n == 1 {
		ch <- provider.ChatDelta{ToolCalls: []types.ToolCall{
			{ID: "1", Name: "write_file", ArgsJSON: `{"path":"main.go","content":"package main\n\nfunc main() { println(\"yeni\") }\n"}`},
			{ID: "2", Name: "write_file", ArgsJSON: `{"path":"docs/README.md","content":"# Kurulum\n"}`},
			{ID: "3", Name: "write_file", ArgsJSON: `{"path":"main.go","content":"package main\n\nfunc main() { println(\"yeni 2\") }\n"}`},
		}}
	} else {
		ch <- provider.ChatDelta{Content: "tamam"}
	}
	ch <- provider.ChatDelta{Done: true}
	close(ch)
	return ch, nil
}

func TestTurnChangesCanBeReviewedAndTakenBack(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	app.Router.Register("editm", &editModel{})
	ag, _ := app.Agents.Upsert(ctx, types.Agent{Name: "e", Provider: "editm", Model: "m"})
	ws := t.TempDir()
	orig := "package main\n\nfunc main() { println(\"eski\") }\n"
	_ = os.WriteFile(filepath.Join(ws, "main.go"), []byte(orig), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("Kurallar"), 0o644)
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
	var chat types.Session
	call(protocol.MethodSessionCreate, map[string]any{"agentId": ag.ID, "space": "chat"}, &chat)
	var res struct{ RunID string }
	if e := call(protocol.MethodSessionSend, map[string]any{"sessionId": chat.ID, "workspace": ws, "content": "düzelt"}, &res); e != "" {
		t.Fatal(e)
	}

	var v editsView
	call(protocol.MethodEditsList, map[string]any{"sessionId": chat.ID}, &v)
	if string(v.RunID) != res.RunID || len(v.Files) != 2 {
		t.Fatalf("edits = %+v (run %s)", v, res.RunID)
	}
	byPath := map[string]editFile{}
	for _, f := range v.Files {
		byPath[f.Path] = f
	}
	mg, rd := byPath["main.go"], byPath["docs/README.md"]
	// two writes to main.go: the diff is against what it held before the turn
	if mg.Change != "modified" || !strings.Contains(mg.Diff, `-func main() { println("eski") }`) || !strings.Contains(mg.Diff, `+func main() { println("yeni 2") }`) || mg.Added != 1 || mg.Removed != 1 {
		t.Fatalf("main.go = %+v", mg)
	}
	if rd.Change != "added" || rd.Status != "pending" {
		t.Fatalf("README = %+v", rd)
	}

	// take main.go back; keep the README
	call(protocol.MethodEditsRevert, map[string]any{"sessionId": chat.ID, "runId": v.RunID, "path": "main.go"}, &v)
	if b, _ := os.ReadFile(filepath.Join(ws, "main.go")); string(b) != orig {
		t.Fatalf("main.go after revert = %q", b)
	}
	call(protocol.MethodEditsAccept, map[string]any{"sessionId": chat.ID, "runId": v.RunID}, &v)
	st := map[string]string{}
	for _, f := range v.Files {
		st[f.Path] = f.Status
	}
	if st["main.go"] != "reverted" || st["docs/README.md"] != "accepted" {
		t.Fatalf("statuses = %v", st)
	}
	if _, err := os.Stat(filepath.Join(ws, "docs/README.md")); err != nil {
		t.Fatal("accepted file is gone")
	}
	// reverting a created file removes it
	call(protocol.MethodEditsRevert, map[string]any{"sessionId": chat.ID, "runId": v.RunID, "path": "docs/README.md"}, nil)
	if _, err := os.Stat(filepath.Join(ws, "docs/README.md")); !os.IsNotExist(err) {
		t.Fatal("created file still there after revert")
	}

	// effort is the chat's, and shows with its model
	if e := call(protocol.MethodSessionSetEffort, map[string]any{"sessionId": chat.ID, "effort": "ultra"}, nil); e == "" {
		t.Fatal("an unknown effort was accepted")
	}
	call(protocol.MethodSessionSetEffort, map[string]any{"sessionId": chat.ID, "effort": "high"}, nil)
	var m sessionModel
	call(protocol.MethodSessionModel, map[string]any{"sessionId": chat.ID}, &m)
	if m.Effort != "high" {
		t.Fatalf("model = %+v", m)
	}
	// the project's rule files are listed for the UI
	var rules struct{ Files []string }
	call(protocol.MethodWorkspaceRules, map[string]any{"workspace": ws}, &rules)
	if len(rules.Files) != 1 || rules.Files[0] != "AGENTS.md" {
		t.Fatalf("rules = %+v", rules)
	}
	// images must be images
	if e := call(protocol.MethodSessionSend, map[string]any{"sessionId": chat.ID, "content": "x", "images": []string{"javascript:alert(1)"}}, nil); e == "" {
		t.Fatal("a non-image was accepted")
	}
}
