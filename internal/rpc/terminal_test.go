//go:build !windows

package rpc

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// A shell opened over HTTP — the browser, or the desktop app on a server
// through its tunnel — is still running once the call has been answered.
func TestTerminalSpawnedOverHTTPKeepsRunning(t *testing.T) {
	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.ServeHTTPOn(ln) }()
	defer s.Close()
	call := func(method string, params any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(params)
		body, _ := json.Marshal(protocol.Request{Method: method, Params: raw, Token: app.Token})
		resp, err := http.Post("http://"+ln.Addr().String()+"/rpc", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out protocol.Response
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || !out.OK {
			t.Fatalf("%s: %v %s", method, err, out.Error)
		}
		return out.Result
	}
	var sess types.TerminalSession
	_ = json.Unmarshal(call(protocol.MethodTerminalSpawn, map[string]any{"command": []string{"/bin/sh", "-c", "sleep 10"}}), &sess)
	time.Sleep(400 * time.Millisecond)
	var list []types.TerminalSession
	_ = json.Unmarshal(call(protocol.MethodTerminalList, nil), &list)
	for _, l := range list {
		if l.ID == sess.ID {
			if l.Status != types.TermRunning {
				t.Fatalf("shell is %s once the spawn call was answered", l.Status)
			}
			return
		}
	}
	t.Fatalf("shell %s not listed", sess.ID)
}
