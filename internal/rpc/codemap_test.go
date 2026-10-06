package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kayra-ML/rove/internal/codemap"
	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func TestCodemapGraphAndShare(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()

	wsDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(wsDir, "src"), 0o755)
	_ = os.WriteFile(filepath.Join(wsDir, "src", "a.ts"), []byte("import { b } from './b';\nexport function a() { b(); }\n"), 0o644)
	_ = os.WriteFile(filepath.Join(wsDir, "src", "b.ts"), []byte("export function b() {}\n"), 0o644)
	ws, err := app.WS.Open(ctx, wsDir, "demo")
	if err != nil {
		t.Fatal(err)
	}

	call := func(method string, params map[string]any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(params)
		resp := s.Dispatch(ctx, protocol.Request{Method: method, Token: app.Token, Params: raw})
		if !resp.OK {
			t.Fatalf("%s: %s", method, resp.Error)
		}
		return resp.Result
	}

	var g codemap.Graph
	_ = json.Unmarshal(call(protocol.MethodMapGraph, map[string]any{"workspaceId": ws.ID}), &g)
	if len(g.Nodes) != 2 || len(g.Edges) != 1 || g.Edges[0].From != "src/a.ts" {
		t.Fatalf("graph = %+v", g)
	}

	agents, _ := app.Agents.List(ctx)
	sess, err := app.Sess.Create(ctx, "t", agents[0].ID, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	call(protocol.MethodMapShare, map[string]any{"sessionId": sess.ID, "files": []string{"src/b.ts"}, "note": "look here"})
	hist, _ := app.Sess.History(ctx, sess.ID)
	if len(hist) != 1 || !strings.Contains(hist[0].Content, "src/b.ts") || !strings.Contains(hist[0].Content, "used by: src/a.ts") {
		t.Fatalf("shared message = %+v", hist)
	}
}
