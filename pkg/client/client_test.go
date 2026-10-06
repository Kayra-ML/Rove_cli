package client

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Kayra-ML/rove/pkg/protocol"
)

// An error the daemon returns over the socket is final: the same request
// must not be replayed over HTTP (a sent message would be sent twice).
func TestCallDoesNotReplayRemoteErrorsOverHTTP(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("unix sockets unavailable:", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			var req protocol.Request
			_ = json.NewDecoder(bufio.NewReader(c)).Decode(&req)
			_ = json.NewEncoder(c).Encode(protocol.Response{OK: false, Error: "profile not found"})
			c.Close()
		}
	}()
	var httpHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&httpHits, 1)
		_ = json.NewEncoder(w).Encode(protocol.Response{OK: true})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.URL, IPC: sock, client: srv.Client()}
	_, err = c.Call("session.send", map[string]any{"content": "hi"})
	if err == nil || !IsRemote(err) || err.Error() != "profile not found" {
		t.Fatalf("err = %v", err)
	}
	if httpHits != 0 {
		t.Fatalf("request replayed over HTTP %d time(s)", httpHits)
	}

	// a dead socket is a transport failure: HTTP is the fallback
	c.IPC = filepath.Join(t.TempDir(), "gone.sock")
	if _, err := c.Call("ping", nil); err != nil || httpHits != 1 {
		t.Fatalf("fallback: err=%v hits=%d", err, httpHits)
	}
}
