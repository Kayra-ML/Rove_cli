//go:build !windows

package terminal

import (
	"bytes"
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/types"
)

func TestSpawnEchoAndKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("conpty covered by build tag on windows")
	}
	bus := eventbus.New()
	defer bus.Close()
	got := make(chan string, 8)
	bus.Subscribe(string(types.EventTerminalData), func(ev types.Event) {
		if d, ok := ev.Payload["data"].(string); ok {
			got <- d
		}
	})
	e := New(nil, bus)
	sess, err := e.Spawn(context.Background(), SpawnOpts{
		Command: []string{"/bin/sh", "-c", "printf hello; sleep 2"},
		Cols:    40, Rows: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sess.PID == 0 {
		t.Fatal("expected pid")
	}
	var buf bytes.Buffer
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case d := <-got:
			buf.WriteString(d)
			if bytes.Contains(buf.Bytes(), []byte("hello")) {
				goto ok
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
ok:
	if !bytes.Contains(buf.Bytes(), []byte("hello")) {
		t.Fatalf("got %q", buf.String())
	}
	if err := e.Kill(sess.ID); err != nil {
		t.Fatal(err)
	}
}

func TestResizeAndList(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	e := New(nil, nil)
	sess, err := e.Spawn(context.Background(), SpawnOpts{Command: []string{"/bin/sh", "-c", "sleep 3"}})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Kill(sess.ID)
	if err := e.Resize(sess.ID, 120, 40); err != nil {
		t.Fatal(err)
	}
	list := e.List()
	if len(list) != 1 {
		t.Fatalf("list %d", len(list))
	}
	_, replay, err := e.Attach(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = replay
}

// A shell outlives the call that opened it. Over HTTP that call's context
// ends as soon as it is answered, which used to kill the shell with it.
func TestShellOutlivesTheCallThatSpawnedIt(t *testing.T) {
	e := New(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := e.Spawn(ctx, SpawnOpts{Command: []string{"/bin/sh", "-c", "sleep 5"}})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Kill(sess.ID)
	cancel() // the RPC answered
	time.Sleep(300 * time.Millisecond)
	for _, s := range e.List() {
		if s.ID == sess.ID && s.Status != types.TermRunning {
			t.Fatalf("shell %s after its call ended", s.Status)
		}
	}
}

func TestCloseKillsEveryShell(t *testing.T) {
	e := New(nil, nil)
	var ids []types.ID
	for i := 0; i < 2; i++ {
		sess, err := e.Spawn(context.Background(), SpawnOpts{Command: []string{"/bin/sh", "-c", "sleep 30"}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sess.ID)
	}
	e.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		running := 0
		for _, s := range e.List() {
			if s.Status == types.TermRunning {
				running++
			}
		}
		if running == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("shells %v still running after Close", ids)
}
