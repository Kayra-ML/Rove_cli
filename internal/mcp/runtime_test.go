package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aether-dev/aether/internal/tool"
)

func TestStartListCall(t *testing.T) {
	rtTools := tool.New(nil)
	r := New(rtTools)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := r.Start(ctx, ServerConfig{Name: "echo", Command: self, Args: []string{"-test.run=TestHelperEcho", "--", "mcp-echo"}, Env: map[string]string{"AETHER_MCP_ECHO": "1"}}); err != nil {
		t.Skipf("helper start: %v", err)
	}
	defer r.Stop("echo")
	if len(r.List()) != 1 {
		t.Fatalf("list %v", r.List())
	}
}

func TestHelperEcho(t *testing.T) {
	if os.Getenv("AETHER_MCP_ECHO") != "1" {
		t.Skip("not helper")
	}
	runEcho()
}

func TestJSONRoundtrip(t *testing.T) {
	b, _ := json.Marshal(rpcReq{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	var r rpcReq
	if err := json.Unmarshal(b, &r); err != nil || r.Method != "initialize" {
		t.Fatal(err)
	}
}
