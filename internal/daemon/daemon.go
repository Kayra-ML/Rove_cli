package daemon

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/rpc"
)

type Daemon struct {
	cfg     config.Config
	app     *core.App
	server  *rpc.Server
	wg      sync.WaitGroup
	release func()
}

// ErrAlreadyRunning means another daemon holds this data dir.
var ErrAlreadyRunning = errors.New("a rovecode daemon is already running for this data dir")

func Start(cfg config.Config) (*Daemon, error) {
	release, err := lockDataDir(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	// Bind the port before anything else: if it is taken, fail here. Serving
	// on in the background would pass the readiness check below against
	// whichever process does own the port.
	ln, err := net.Listen("tcp", cfg.ListenHTTP)
	if err != nil {
		release()
		return nil, fmt.Errorf("http %s: %w", cfg.ListenHTTP, err)
	}
	app, err := core.Open(cfg)
	if err != nil {
		ln.Close()
		release()
		return nil, err
	}
	d := &Daemon{cfg: cfg, app: app, server: rpc.New(app), release: release}
	d.wg.Add(2)
	go func() {
		defer d.wg.Done()
		if err := d.server.ServeHTTPOn(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

func (d *Daemon) Stop() error {
	_ = d.server.Close()
	err := d.app.Close()
	d.wg.Wait()
	if d.release != nil {
		d.release()
	}
	return err
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
	legacy := filepath.Join(dataDir, "aetherd.pid")
	modern := filepath.Join(dataDir, "rovecode.pid")
	if _, err := os.Stat(modern); err == nil {
		return modern
	}
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return modern
}

func WritePID(dataDir string) error {
	return os.WriteFile(PIDPath(dataDir), []byte(fmt.Sprintf("%d", os.Getpid())), 0o600)
}
