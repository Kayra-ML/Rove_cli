//go:build !windows

package tui

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/pkg/protocol"
)

// slowDaemon answers every call on a unix socket after delay.
func slowDaemon(t *testing.T, delay time.Duration) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tui")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				var req protocol.Request
				if json.NewDecoder(bufio.NewReader(c)).Decode(&req) != nil {
					return
				}
				time.Sleep(delay)
				_ = json.NewEncoder(c).Encode(protocol.Response{ID: req.ID, OK: true, Result: json.RawMessage(`{}`)})
			}()
		}
	}()
	return path
}

// A chat turn is answered when it is done; it is not shown as failed for
// taking longer than an ordinary call may.
func TestSendWaitsForALongTurn(t *testing.T) {
	prev := callWait
	callWait = 100 * time.Millisecond
	defer func() { callWait = prev }()
	c := &IPCClient{ipcPath: slowDaemon(t, 400*time.Millisecond), httpBase: "http://127.0.0.1:1"}
	if _, err := c.Call(protocol.MethodSessionList, nil); err == nil {
		t.Fatal("an ordinary call should give up after callWait")
	}
	msg := c.SendMessage("s1", "hi")().(SendMsg)
	if msg.Err != nil {
		t.Fatalf("a long turn was reported as failed: %v", msg.Err)
	}
}
