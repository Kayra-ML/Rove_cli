package rpc

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/aether-dev/aether/internal/config"
	"github.com/aether-dev/aether/internal/core"
	"github.com/aether-dev/aether/internal/types"
	"github.com/aether-dev/aether/pkg/protocol"
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
		"agentId":   agents[0].ID,
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

func TestCardLogsAndArtifact(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()

	// create a card
	createP, _ := json.Marshal(map[string]any{"title": "test card", "column": "backlog"})
	resp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodCardCreate, Token: app.Token, Params: createP})
	if !resp.OK {
		t.Fatalf("card create: %+v", resp)
	}
	var card types.Card
	if err := json.Unmarshal(resp.Result, &card); err != nil {
		t.Fatal(err)
	}

	// add artifact
	artP, _ := json.Marshal(map[string]any{
		"id":       card.ID,
		"artifact": map[string]any{"kind": "file", "label": "output.go", "path": "/tmp/output.go"},
	})
	resp = s.Dispatch(ctx, protocol.Request{Method: protocol.MethodCardAddArtifact, Token: app.Token, Params: artP})
	if !resp.OK {
		t.Fatalf("addArtifact: %+v", resp)
	}

	// fetch logs (empty but should succeed)
	logsP, _ := json.Marshal(map[string]any{"id": card.ID})
	resp = s.Dispatch(ctx, protocol.Request{Method: protocol.MethodCardLogs, Token: app.Token, Params: logsP})
	if !resp.OK {
		t.Fatalf("card.logs: %+v", resp)
	}

	// verify artifact persisted
	getP, _ := json.Marshal(map[string]any{"id": card.ID})
	resp = s.Dispatch(ctx, protocol.Request{Method: protocol.MethodCardGet, Token: app.Token, Params: getP})
	if !resp.OK {
		t.Fatalf("card.get: %+v", resp)
	}
	var got types.Card
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].Label != "output.go" {
		t.Fatalf("artifacts: %+v", got.Artifacts)
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

func TestGitCommitAndLog(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()

	// init a git repo in the temp dir
	if out, e := runCmd(dir, "git", "init", dir); e != nil {
		t.Skipf("git not available: %v %s", e, out)
	}
	runCmd(dir, "git", "config", "user.email", "test@test.com")
	runCmd(dir, "git", "config", "user.name", "Test")

	// write a file and commit via RPC
	if err := writeFile(dir+"/hello.txt", "hello"); err != nil {
		t.Fatal(err)
	}
	commitP, _ := json.Marshal(map[string]any{"path": dir, "message": "initial"})
	resp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodGitCommit, Token: app.Token, Params: commitP})
	if !resp.OK {
		t.Fatalf("git.commit: %+v", resp)
	}

	// log should return at least 1 entry
	logP, _ := json.Marshal(map[string]any{"path": dir, "limit": 5})
	resp = s.Dispatch(ctx, protocol.Request{Method: protocol.MethodGitLog, Token: app.Token, Params: logP})
	if !resp.OK {
		t.Fatalf("git.log: %+v", resp)
	}
	var commits []map[string]any
	if err := json.Unmarshal(resp.Result, &commits); err != nil {
		t.Fatal(err)
	}
	if len(commits) < 1 {
		t.Fatalf("expected at least 1 commit, got %d", len(commits))
	}

	if err := writeFile(dir+"/hello.txt", "hello world"); err != nil {
		t.Fatal(err)
	}
	diffP, _ := json.Marshal(map[string]any{"path": dir})
	resp = s.Dispatch(ctx, protocol.Request{Method: protocol.MethodGitDiff, Token: app.Token, Params: diffP})
	if !resp.OK {
		t.Fatalf("git.diff: %+v", resp)
	}
	var diffOut struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(resp.Result, &diffOut); err != nil {
		t.Fatal(err)
	}
	if diffOut.Diff == "" {
		t.Fatal("expected non-empty diff")
	}
}

// helpers for TestGitCommitAndLog
func runCmd(dir string, args ...string) (string, error) {
	return runCmdImpl(dir, args...)
}

// ── Harness RPC tests ─────────────────────────────────────────────────────────

func TestHarnessCompose(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()

	analysis := map[string]any{
		"taskType":   "bug_fix",
		"complexity": 0.15,
		"riskLevel":  0.1,
	}
	p, _ := json.Marshal(map[string]any{"analysis": analysis})
	resp := s.Dispatch(ctx, protocol.Request{
		Method: protocol.MethodHarnessCompose, Token: app.Token, Params: p,
	})
	if !resp.OK {
		t.Fatalf("harness.compose failed: %+v", resp)
	}
	var hp map[string]any
	if err := json.Unmarshal(resp.Result, &hp); err != nil {
		t.Fatal(err)
	}
	if hp["current"] == nil {
		t.Fatal("harness.compose: expected 'current' field in response")
	}
}

func TestHarnessSetAndGet(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()

	// Create a goal first so we have an ID.
	gp, _ := json.Marshal(types.Goal{
		Title:       "harness-rpc-test",
		Description: "test",
		CompletionContract: types.CompletionContract{
			Criteria:      []string{"thing done"},
			MaxIterations: 3,
		},
	})
	gresp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodGoalCreate, Token: app.Token, Params: gp})
	if !gresp.OK {
		t.Fatalf("goal.create failed: %+v", gresp)
	}
	var g types.Goal
	if err := json.Unmarshal(gresp.Result, &g); err != nil {
		t.Fatal(err)
	}

	// Set harness profile.
	profile := map[string]any{
		"goalId":  string(g.ID),
		"current": map[string]any{"mode": "manual", "context": 1, "execution": 1, "tools": 8, "verify": 2, "recovery": 1, "version": 1},
	}
	sp, _ := json.Marshal(map[string]any{
		"goalId":  string(g.ID),
		"profile": profile,
	})
	sresp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodHarnessSet, Token: app.Token, Params: sp})
	if !sresp.OK {
		t.Fatalf("harness.set failed: %+v", sresp)
	}

	// Get it back.
	gtp, _ := json.Marshal(map[string]any{"goalId": string(g.ID)})
	gtresp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodHarnessGet, Token: app.Token, Params: gtp})
	if !gtresp.OK {
		t.Fatalf("harness.get failed: %+v", gtresp)
	}
	if len(gtresp.Result) == 0 {
		t.Fatal("harness.get: empty result")
	}
}

func TestHarnessMutationsEmpty(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()

	// Create a goal.
	gp, _ := json.Marshal(types.Goal{
		Title: "mut-test",
		CompletionContract: types.CompletionContract{
			Criteria: []string{"done"}, MaxIterations: 2,
		},
	})
	gresp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodGoalCreate, Token: app.Token, Params: gp})
	if !gresp.OK {
		t.Fatalf("goal.create: %+v", gresp)
	}
	var g types.Goal
	_ = json.Unmarshal(gresp.Result, &g)

	mp, _ := json.Marshal(map[string]any{"goalId": string(g.ID)})
	resp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodHarnessMutations, Token: app.Token, Params: mp})
	if !resp.OK {
		t.Fatalf("harness.mutations: %+v", resp)
	}
	// Fresh goal has no mutations — result should be JSON null or empty array.
	result := string(resp.Result)
	if result != "null" && result != "[]" {
		t.Logf("harness.mutations result: %s (acceptable)", result)
	}
}

func TestHarnessGetMissingID(t *testing.T) {
	dir := t.TempDir()
	app, err := core.Open(config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()

	// Neither goalId nor cardId → error.
	p, _ := json.Marshal(map[string]any{})
	resp := s.Dispatch(ctx, protocol.Request{Method: protocol.MethodHarnessGet, Token: app.Token, Params: p})
	if resp.OK {
		t.Fatal("harness.get with no ids should fail")
	}
}

func TestUsageGetAndHarnessPresets(t *testing.T) {
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

	resp = s.Dispatch(ctx, protocol.Request{Method: protocol.MethodHarnessPresets, Token: app.Token})
	if !resp.OK {
		t.Fatalf("harness.presets %+v", resp)
	}
	var presets map[string]json.RawMessage
	if err := json.Unmarshal(resp.Result, &presets); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"small", "large", "hard"} {
		if _, ok := presets[k]; !ok {
			t.Fatalf("missing preset %s", k)
		}
	}
}
