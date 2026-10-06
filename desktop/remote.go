package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/remote"
	"github.com/Kayra-ML/rove/pkg/client"
	"github.com/Kayra-ML/rove/pkg/protocol"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// remoteBins holds the Linux builds of rovecode installed on servers
// (made by scripts/remote-bins.sh before each build).
//
//go:embed remotebin
var remoteBins embed.FS

func bundledBinary(goos, goarch string) ([]byte, error) {
	b, err := remoteBins.ReadFile("remotebin/rovecode-" + goos + "-" + goarch + ".gz")
	if err != nil {
		return nil, fmt.Errorf("this build of the app carries no rovecode for %s/%s", goos, goarch)
	}
	return b, nil
}

// the connection to a server's daemon, when there is one
var (
	connMu sync.Mutex
	conn   *remote.Conn
)

// Connection is where the app is connected: "local", or a server's state.
func (a *App) Connection() remote.State {
	connMu.Lock()
	defer connMu.Unlock()
	if conn == nil {
		return remote.State{Status: "local"}
	}
	return conn.State()
}

// ConnectRemote runs Rove on a server and points the app at it. The target
// is JSON: {"host","user","port","keyPath"}. Progress and later changes
// (reconnecting, connected again) arrive as "rove:conn" events.
func (a *App) ConnectRemote(targetJSON string) (remote.State, error) {
	var t remote.Target
	if err := json.Unmarshal([]byte(targetJSON), &t); err != nil {
		return remote.State{Status: "error", Message: err.Error()}, err
	}
	if strings.TrimSpace(t.Host) == "" {
		return remote.State{Status: "error", Message: "no host"}, fmt.Errorf("no host")
	}
	a.DisconnectRemote()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	c, err := remote.Dial(ctx, t, remote.SSH{Target: t}, bundledBinary, protocol.APILevel, remote.Options{
		Progress: func(step string) { a.emit("rove:conn-progress", step) },
		OnState:  a.remoteState,
	})
	if err != nil {
		return remote.State{Status: "error", Host: t.Label(), Message: err.Error()}, err
	}
	connMu.Lock()
	conn = c
	connMu.Unlock()
	st := c.State()
	a.remoteState(st)
	return st, nil
}

// DisconnectRemote goes back to this computer's daemon. The server's daemon
// keeps running (its chats stay there).
func (a *App) DisconnectRemote() {
	connMu.Lock()
	c := conn
	conn = nil
	connMu.Unlock()
	if c != nil {
		c.Close()
	}
	a.setClient(nil) // RPC reconnects to the local daemon
	a.emit("rove:conn", remote.State{Status: "local"})
}

// remoteState follows the connection: RPC goes to the server while it is
// connected, and the page is told (a new tunnel may have a new address).
func (a *App) remoteState(s remote.State) {
	if s.Status == "connected" && s.HTTP != "" {
		a.setClient(client.NewHTTP(s.HTTP, s.Token))
	}
	a.emit("rove:conn", s)
}

func (a *App) emit(name string, data any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, data)
	}
}

// remoteAddr is the daemon's address and token while connected to a server.
func remoteAddr() (string, string, bool) {
	connMu.Lock()
	defer connMu.Unlock()
	if conn == nil {
		return "", "", false
	}
	st := conn.State()
	if st.HTTP == "" {
		return "", "", false
	}
	return strings.TrimPrefix(st.HTTP, "http://"), st.Token, true
}

// shutdown closes the tunnel when the app quits.
func (a *App) shutdown(context.Context) {
	connMu.Lock()
	c := conn
	conn = nil
	connMu.Unlock()
	if c != nil {
		c.Close()
	}
}
