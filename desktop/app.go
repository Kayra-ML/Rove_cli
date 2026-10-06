package main

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/daemon"
	"github.com/Kayra-ML/rove/pkg/client"
	"github.com/Kayra-ML/rove/pkg/protocol"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is a presentation-layer bridge. It never implements agent logic.
type App struct {
	ctx      context.Context
	cfg      config.Config
	clientMu sync.Mutex
	client   *client.Client // the daemon RPC goes to (a server's while connected)
}

func (a *App) getClient() *client.Client {
	a.clientMu.Lock()
	defer a.clientMu.Unlock()
	return a.client
}

func (a *App) setClient(c *client.Client) {
	a.clientMu.Lock()
	a.client = c
	a.clientMu.Unlock()
}

func NewApp(cfg config.Config) *App { return &App{cfg: cfg} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	_ = a.cfg.EnsureDirs()
	replaceStaleDaemon()
	ensureDaemon()
	cl, err := client.FromEnv()
	if err == nil {
		a.setClient(cl)
	}
}

func (a *App) Token() string {
	if _, tok, ok := remoteAddr(); ok {
		return tok
	}
	cl := a.getClient()
	if cl == nil {
		return ""
	}
	return cl.Token
}

func (a *App) HTTPAddr() string {
	if addr, _, ok := remoteAddr(); ok {
		return addr
	}
	return a.cfg.ListenHTTP
}

func (a *App) PickFolder() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open workspace",
	})
}

func (a *App) RPC(method string, paramsJSON string) (string, error) {
	cl := a.getClient()
	if cl == nil {
		var err error
		if cl, err = client.FromEnv(); err != nil {
			return "", err
		}
		a.setClient(cl)
	}
	var params any
	if paramsJSON != "" && paramsJSON != "null" {
		if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
			return "", err
		}
	}
	res, err := cl.Call(method, params)
	if _, _, onServer := remoteAddr(); onServer {
		// a server's daemon is kept up by the connection, not started here
		if err != nil {
			b, _ := json.Marshal(protocol.Response{OK: false, Error: err.Error()})
			return string(b), nil
		}
		b, _ := json.Marshal(protocol.Response{OK: true, Result: res})
		return string(b), nil
	}
	if err != nil && !client.IsRemote(err) && !pingDaemon() {
		// The daemon is gone (crashed, killed by an update, never started):
		// bring it back and try once more. Only when a ping fails too — a
		// long request that merely timed out must not be sent twice.
		ensureDaemon()
		if fresh, cerr := client.FromEnv(); cerr == nil {
			a.setClient(fresh)
			res, err = fresh.Call(method, params)
		}
	}
	if err != nil {
		b, _ := json.Marshal(protocol.Response{OK: false, Error: err.Error()})
		return string(b), nil
	}
	b, _ := json.Marshal(protocol.Response{OK: true, Result: res})
	return string(b), nil
}

// daemonMu makes ensureDaemon one-at-a-time. The webview fires many RPCs at
// once; without it each one that found the daemon down started its own.
var daemonMu sync.Mutex

func ensureDaemon() {
	daemonMu.Lock()
	defer daemonMu.Unlock()
	// whoever held the lock may have brought it up already
	if pingDaemon() {
		return
	}
	cmd := daemon.Command()
	if cmd == nil {
		return
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return
	}
	_ = cmd.Process.Release()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pingDaemon() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func pingDaemon() bool {
	cl, err := client.FromEnv()
	if err != nil {
		return false
	}
	_, err = cl.Call(protocol.MethodPing, nil)
	return err == nil
}
