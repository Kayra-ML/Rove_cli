//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/daemon"
)

// The daemon commands against a real daemon, run in a process of its own
// as `rovecode daemon` would be: status reads it, logs shows its log, stop
// stops it cleanly and waits until it is gone.
func TestDaemonCommands(t *testing.T) {
	if os.Getenv("ROVECODE_TEST_DAEMON") == "1" {
		runBundledDaemon()
		return
	}
	home := t.TempDir()
	socks, err := os.MkdirTemp("/tmp", "rdc")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socks)
	conf, _ := json.Marshal(map[string]string{"listenHttp": "127.0.0.1:17441", "listenIpc": filepath.Join(socks, "d.sock")})
	if err := os.WriteFile(filepath.Join(home, "config.json"), conf, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ROVECODE_HOME", home)
	cfg, err := config.Load("")
	if err != nil || cfg.ListenHTTP != "127.0.0.1:17441" {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
	var out bytes.Buffer
	if code := daemonStatus(&out, cfg, false); code != exitNotRunning || !strings.Contains(out.String(), "not running") {
		t.Fatalf("status before start = %d %q", code, out.String())
	}

	child := exec.Command(os.Args[0], "-test.run=^TestDaemonCommands$")
	child.Env = append(os.Environ(), "ROVECODE_TEST_DAEMON=1", "ROVECODE_HOME="+home, "SHELL=/bin/sh")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	t.Cleanup(func() { _ = child.Process.Kill() })
	for end := time.Now().Add(20 * time.Second); !daemonHealthy(cfg.ListenHTTP); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("the daemon did not come up")
		}
	}
	pid := strconv.Itoa(child.Process.Pid)

	out.Reset()
	if code := daemonStatus(&out, cfg, false); code != 0 || !strings.Contains(out.String(), "running (pid "+pid) || !strings.Contains(out.String(), "goals") {
		t.Fatalf("status = %d %q", code, out.String())
	}
	out.Reset()
	daemonStatus(&out, cfg, true)
	var h map[string]any
	if err := json.Unmarshal(out.Bytes(), &h); err != nil || h["running"] != true || intOf(h["pid"]) != child.Process.Pid {
		t.Fatalf("status --json = %s", out.String())
	}

	out.Reset()
	if code := daemonLogs(&out, cfg, []string{"-n", "20"}); code != 0 || !strings.Contains(out.String(), "daemon dev started: pid "+pid) {
		t.Fatalf("logs = %d %q", code, out.String())
	}

	out.Reset()
	if code := daemonStop(&out, cfg); code != 0 || !strings.Contains(out.String(), "stopped") {
		t.Fatalf("stop = %d %q", code, out.String())
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("stop returned with the daemon still running")
	}
	// it stopped cleanly: its pid file is gone and its log says so
	if _, err := os.Stat(daemon.PIDPath(home)); !os.IsNotExist(err) {
		t.Fatalf("pid file left after a clean stop: %v", err)
	}
	lines, _, _ := tailLines(daemon.LogPath(home), 5)
	if !strings.Contains(strings.Join(lines, "\n"), "daemon stopped") {
		t.Fatalf("log after stop = %q", lines)
	}
	out.Reset()
	if code := daemonStop(&out, cfg); code != 0 || !strings.Contains(out.String(), "not running") {
		t.Fatalf("second stop = %d %q", code, out.String())
	}
}

func TestTailAndFollowTheLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d.log")
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		b.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	_ = os.WriteFile(p, []byte(b.String()), 0o600)
	lines, size, err := tailLines(p, 3)
	if err != nil || strings.Join(lines, ",") != "line 98,line 99,line 100" {
		t.Fatalf("tail = %v %v", lines, err)
	}
	var out syncBuffer
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { followFile(p, size, &out, stop, 10*time.Millisecond); close(done) }()
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString("line 101\n")
	f.Close()
	waitText(t, &out, "line 101\n")
	// set aside (rotated): the new file is read from its start
	_ = os.WriteFile(p, []byte("fresh\n"), 0o600)
	waitText(t, &out, "fresh\n")
	close(stop)
	<-done
}

// syncBuffer is written by followFile and read by the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func waitText(t *testing.T, b *syncBuffer, want string) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); !strings.Contains(b.String(), want); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("never printed %q; got %q", want, b.String())
		}
	}
}
