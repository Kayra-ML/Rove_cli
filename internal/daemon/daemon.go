package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/aether-dev/aether/internal/config"
	"github.com/aether-dev/aether/internal/core"
	"github.com/aether-dev/aether/internal/rpc"
)

type Daemon struct {
	cfg    config.Config
	app    *core.App
	server *rpc.Server
	wg     sync.WaitGroup
}

func Start(cfg config.Config) (*Daemon, error) {
	app, err := core.Open(cfg)
	if err != nil {
		return nil, err
	}
	d := &Daemon{cfg: cfg, app: app, server: rpc.New(app)}
	d.wg.Add(2)
	go func() {
		defer d.wg.Done()
		if err := d.server.ServeHTTP(cfg.ListenHTTP); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "http: %v\n", err)
		}
	}()
	go func() {
		defer d.wg.Done()
		if err := d.server.ServeIPC(cfg.ListenIPC); err != nil {
			fmt.Fprintf(os.Stderr, "ipc: %v\n", err)
		}
	}()
	if err := waitHTTP(cfg.ListenHTTP, 5*time.Second); err != nil {
		_ = d.Stop()
		return nil, err
	}
	return d, nil
}

func (d *Daemon) App() *core.App { return d.app }

func (d *Daemon) Stop() error {
	_ = d.server.Close()
	err := d.app.Close()
	d.wg.Wait()
	return err
}

func (d *Daemon) Wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	_ = d.Stop()
}

func waitHTTP(addr string, d time.Duration) error {
	deadline := time.Now().Add(d)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	url := "http://" + addr + "/health"
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := net.DialTimeout("tcp", addr, time.Second); err != nil {
		return fmt.Errorf("daemon http not ready on %s: %w", addr, err)
	}
	return nil
}

func PIDPath(dataDir string) string {
	return filepath.Join(dataDir, "aetherd.pid")
}

func WritePID(dataDir string) error {
	return os.WriteFile(PIDPath(dataDir), []byte(fmt.Sprintf("%d", os.Getpid())), 0o600)
}

func RunForeground(ctx context.Context, cfg config.Config) error {
	d, err := Start(cfg)
	if err != nil {
		return err
	}
	_ = WritePID(cfg.DataDir)
	defer os.Remove(PIDPath(cfg.DataDir))
	done := make(chan struct{})
	go func() {
		d.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return d.Stop()
	case <-done:
		return nil
	}
}
