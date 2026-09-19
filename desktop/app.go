package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/daemon"
	"github.com/Kayra-ML/rove/pkg/client"
	"github.com/Kayra-ML/rove/pkg/protocol"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is a presentation-layer bridge. It never implements agent logic.
type App struct {
	ctx    context.Context
	cfg    config.Config
	client *client.Client
}

func NewApp(cfg config.Config) *App { return &App{cfg: cfg} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	_ = a.cfg.EnsureDirs()
	ensureDaemon()
	cl, err := client.FromEnv()
	if err == nil {
		a.client = cl
	}
}

func (a *App) Token() string {
	if a.client == nil {
		return ""
	}
	return a.client.Token
}

func (a *App) HTTPAddr() string {
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
	if a.client == nil {
		cl, err := client.FromEnv()
		if err != nil {
			return "", err
		}
		a.client = cl
	}
	var params any
	if paramsJSON != "" && paramsJSON != "null" {
		if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
			return "", err
		}
	}
	res, err := a.client.Call(method, params)
	if err != nil {
		b, _ := json.Marshal(protocol.Response{OK: false, Error: err.Error()})
		return string(b), nil
	}
	b, _ := json.Marshal(protocol.Response{OK: true, Result: res})
	return string(b), nil
}

func ensureDaemon() {
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
