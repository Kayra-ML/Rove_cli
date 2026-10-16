//go:build !windows

package provider

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Stopping a run stops what the agent system started, not only the CLI.
func TestAgentCLIStopKillsWhatItStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	script := `#!/bin/sh
sleep 30 &
echo $! > "` + pidFile + `"
echo '{"type":"system","subtype":"init","session_id":"s-1"}'
sleep 30
`
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := &AgentCLI{System: SysClaudeCode, Access: "edits"}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := a.Complete(ctx, ChatRequest{Workdir: t.TempDir(), Messages: []ChatMessage{{Role: "user", Content: "go"}}})
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	for i := 0; i < 50 && pid == 0; i++ {
		time.Sleep(50 * time.Millisecond)
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
	}
	if pid == 0 {
		t.Fatal("the stand-in never started its child")
	}
	cancel()
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not end after it was stopped")
	}
	for i := 0; i < 40; i++ {
		if syscall.Kill(pid, 0) != nil {
			return // gone
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatal("the child the agent started outlived the stopped run")
}
