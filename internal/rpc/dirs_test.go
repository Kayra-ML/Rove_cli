package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func TestDirsListsFoldersOnly(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	root := t.TempDir()
	for _, d := range []string{"api", "Web", ".cache", "node_modules"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x"), 0o644)
	raw, _ := json.Marshal(map[string]any{"path": root})
	resp := s.Dispatch(context.Background(), protocol.Request{Method: protocol.MethodFileDirs, Token: app.Token, Params: raw})
	if !resp.OK {
		t.Fatal(resp.Error)
	}
	var v dirList
	_ = json.Unmarshal(resp.Result, &v)
	if len(v.Dirs) != 2 || v.Dirs[0] != "api" || v.Dirs[1] != "Web" || !v.Project || v.Parent != filepath.Dir(root) {
		t.Fatalf("dirs = %+v", v)
	}
	raw, _ = json.Marshal(map[string]any{"path": filepath.Join(root, "missing")})
	if resp := s.Dispatch(context.Background(), protocol.Request{Method: protocol.MethodFileDirs, Token: app.Token, Params: raw}); resp.OK {
		t.Fatal("a missing folder listed")
	}
}
