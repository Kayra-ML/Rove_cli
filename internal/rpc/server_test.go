package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func TestDispatchAuthAndPing(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir, ListenHTTP: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	resp := s.Dispatch(context.Background(), protocol.Request{Method: protocol.MethodPing})
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	resp = s.Dispatch(context.Background(), protocol.Request{Method: protocol.MethodAgentList})
	if resp.OK {
		t.Fatal("expected unauthorized")
	}
	resp = s.Dispatch(context.Background(), protocol.Request{Method: protocol.MethodAgentList, Token: app.Token})
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	var agents []types.Agent
	if err := json.Unmarshal(resp.Result, &agents); err != nil || len(agents) == 0 {
		t.Fatalf("agents %v %v", agents, err)
	}
}

func TestSSEFlushesHandshakeImmediately(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	s := New(app)
	httpServer := httptest.NewServer(http.HandlerFunc(s.handleSSE))
	defer httpServer.Close()

	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(httpServer.URL + "?token=" + app.Token)
	if err != nil {
		t.Fatalf("SSE handshake blocked: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatalf("read handshake: %v", err)
	}
	if line != ": connected\n" {
		t.Fatalf("handshake = %q", line)
	}
}

func TestUnixRoundTrip(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	sock := filepath.Join(dir, "aether.sock")
	errCh := make(chan error, 1)
	go func() { errCh <- s.ServeIPC(sock) }()
	deadline := time.Now().Add(2 * time.Second)
	var c net.Conn
	for time.Now().Before(deadline) {
		c, err = net.Dial("unix", sock)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if c == nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	enc := json.NewEncoder(c)
	dec := json.NewDecoder(c)
	if err := enc.Encode(protocol.Request{Method: protocol.MethodPing, Token: app.Token}); err != nil {
		t.Fatal(err)
	}
	var resp protocol.Response
	if err := dec.Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	_ = s.Close()
}

func TestSessionSendUsesCore(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	agents, _ := app.Agents.List(ctx)
	sess, err := app.Sess.Create(ctx, "t", agents[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(map[string]any{
		"sessionId": sess.ID,
		"content":   "hello",
	})
	resp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodSessionSend, Token: app.Token, Params: params})
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	hist, err := app.Sess.History(ctx, sess.ID)
	if err != nil || len(hist) < 2 {
		t.Fatalf("hist %v %v", hist, err)
	}
}

func TestSessionCreateBindsCwdWorkspace(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	cwd := t.TempDir()
	params, _ := json.Marshal(map[string]any{"cwd": cwd, "title": "from-tui"})
	resp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodSessionCreate, Token: app.Token, Params: params})
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	var sess types.Session
	if err := json.Unmarshal(resp.Result, &sess); err != nil {
		t.Fatal(err)
	}
	if sess.WorkspaceID == "" {
		t.Fatal("session created without workspace")
	}
	ws, err := app.WS.Get(ctx, sess.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Path != cwd && filepath.Clean(ws.Path) != filepath.Clean(cwd) {
		t.Fatalf("workspace path = %s want %s", ws.Path, cwd)
	}
	if sess.AgentID == "" {
		t.Fatal("session created without agent")
	}
}

func TestSessionSendRenamesPlaceholderTitle(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	agents, _ := app.Agents.List(ctx)
	sess, err := app.Sess.Create(ctx, "", agents[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(map[string]any{
		"sessionId": sess.ID,
		"content":   "fix the failing auth test",
	})
	resp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodSessionSend, Token: app.Token, Params: params})
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	got, err := app.Sess.Get(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Fix the failing auth test" {
		t.Fatalf("title = %q", got.Title)
	}
}

func TestAgentUpsertAndDelete(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()

	// upsert new agent
	agP, _ := json.Marshal(map[string]any{
		"name": "test-agent", "provider": "fake", "model": "fake",
	})
	resp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodAgentUpsert, Token: app.Token, Params: agP})
	if !resp.OK {
		t.Fatalf("upsert: %+v", resp)
	}
	var ag types.Agent
	if err := json.Unmarshal(resp.Result, &ag); err != nil {
		t.Fatal(err)
	}

	// delete it
	delP, _ := json.Marshal(map[string]any{"id": ag.ID})
	resp = s.Dispatch(ctx, protocol.Request{Method: protocol.MethodAgentDelete, Token: app.Token, Params: delP})
	if !resp.OK {
		t.Fatalf("delete: %+v", resp)
	}

	// list should not include it
	resp = s.Dispatch(ctx, protocol.Request{Method: protocol.MethodAgentList, Token: app.Token})
	if !resp.OK {
		t.Fatalf("list: %+v", resp)
	}
	var agents []types.Agent
	_ = json.Unmarshal(resp.Result, &agents)
	for _, a := range agents {
		if a.ID == ag.ID {
			t.Fatal("deleted agent still in list")
		}
	}
}

func TestUsageGet(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()

	resp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodUsageGet, Token: app.Token})
	if !resp.OK {
		t.Fatalf("usage.get %+v", resp)
	}
	var health map[string]any
	if err := json.Unmarshal(resp.Result, &health); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"totalTokens", "activeAgents", "days", "ok"} {
		if _, ok := health[k]; !ok {
			t.Fatalf("missing %s in %v", k, health)
		}
	}

}
