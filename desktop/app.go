package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/aether-dev/aether/internal/config"
	"github.com/aether-dev/aether/pkg/client"
	"github.com/aether-dev/aether/pkg/protocol"
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
	ensureDaemon(a.cfg)
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

func ensureDaemon(cfg config.Config) {
	cl, err := client.FromEnv()
	if err == nil {
		if _, err := cl.Call(protocol.MethodPing, nil); err == nil {
			return
		}
	}
	candidates := []string{"aetherd"}
	if self, err := os.Executable(); err == nil {
		candidates = append([]string{filepath.Join(filepath.Dir(self), "aetherd")}, candidates...)
	}
	for _, bin := range candidates {
		cmd := exec.Command(bin)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		if err := cmd.Start(); err == nil {
			time.Sleep(500 * time.Millisecond)
			return
		}
	}
}
