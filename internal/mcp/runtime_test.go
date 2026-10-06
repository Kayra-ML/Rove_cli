package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/Kayra-ML/rove/internal/tool"
)

func echoConfig(t *testing.T, name string) ServerConfig {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return ServerConfig{Name: name, Command: self, Args: []string{"-test.run=TestHelperEcho", "--", "mcp-echo"}, Env: map[string]string{"AETHER_MCP_ECHO": "1"}}
}

// A server's tools reach the agents under a provider-safe name, answer as
// text, and go away with the server.
func TestStartCallStop(t *testing.T) {
	tools := tool.New(nil)
	r := New(tools)
	ctx := context.Background()
	if err := r.Start(ctx, echoConfig(t, "My Echo")); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := r.List(); len(got) != 1 || got[0] != "My Echo" {
		t.Fatalf("list %v", got)
	}
	specs := tools.Specs()
	if len(specs) != 1 || specs[0].Name != "mcp_my-echo_ping_now" {
		t.Fatalf("specs %+v", specs)
	}
	res, err := tools.Call(ctx, "mcp_my-echo_ping_now", tool.Context{}, json.RawMessage(`{}`))
	if err != nil || res.Content != "pong" || res.IsError {
		t.Fatalf("call %+v %v", res, err)
	}
	// starting again replaces the running one
	if err := r.Start(ctx, echoConfig(t, "My Echo")); err != nil || len(r.List()) != 1 {
		t.Fatalf("restart %v %v", err, r.List())
	}
	if err := r.Stop("My Echo"); err != nil {
		t.Fatal(err)
	}
	if len(tools.Specs()) != 0 || len(r.List()) != 0 {
		t.Fatalf("left behind %v %v", tools.Specs(), r.List())
	}
}

// A command that is not an MCP server fails to start and says why.
func TestStartFails(t *testing.T) {
	r := New(tool.New(nil))
	if err := r.Start(context.Background(), ServerConfig{Name: "bad", Command: "/usr/bin/true"}); err == nil {
		t.Fatal("started")
	}
	if r.Errors()["bad"] == "" || len(r.List()) != 0 {
		t.Fatalf("errors %v list %v", r.Errors(), r.List())
	}
}

func TestHelperEcho(t *testing.T) {
	if os.Getenv("AETHER_MCP_ECHO") != "1" {
		t.Skip("not helper")
	}
	runEcho()
}

func TestCallText(t *testing.T) {
	if s, e := callText(json.RawMessage(`{"content":[{"type":"text","text":"a"},{"type":"image"},{"type":"text","text":"b"}],"isError":true}`)); s != "a\nb" || !e {
		t.Fatal(s, e)
	}
	if s, _ := callText(json.RawMessage(`{"x":1}`)); s != `{"x":1}` {
		t.Fatal(s)
	}
}
