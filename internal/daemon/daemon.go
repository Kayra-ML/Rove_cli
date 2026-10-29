package daemon

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	// done is closed when the daemon is asked to stop (the shutdown
	// method) and when it stops
	done     chan struct{}
	doneOnce sync.Once
	stopOnce sync.Once
	stopErr  error
}

// ErrAlreadyRunning means another daemon holds this data dir.
var ErrAlreadyRunning = errors.New("a rovecode daemon is already running for this data dir")

// lockWait is how long a starting daemon waits for one that is stopping —
// no longer serving, still closing its store — to let go of the data dir.
var lockWait = 10 * time.Second

// lock takes the data dir. A holder that still answers is a running daemon
// and refuses at once; one that does not is on its way out and is waited
// for: an app that restarts the daemon starts the new one as soon as the
// old stops answering, while the old is still putting things away.
func lock(cfg config.Config) (func(), error) {
	deadline := time.Now().Add(lockWait)
	for {
		release, err := lockDataDir(cfg.DataDir, cfg.ListenHTTP)
		if !errors.Is(err, ErrAlreadyRunning) {
			return release, err
		}
		if holderServes(cfg.DataDir) || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// holderServes reports whether the daemon holding the data dir answers on
// the address it wrote in the lock file.
func holderServes(dataDir string) bool {
	_, addr := lockHolder(dataDir)
	if addr == "" {
		return false
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://" + addr + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// lockHolder reads the pid and HTTP address the holder wrote.
func lockHolder(dataDir string) (pid, addr string) {
	b, _ := os.ReadFile(filepath.Join(dataDir, "rovecode.lock"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	pid = strings.TrimSpace(lines[0])
	if len(lines) > 1 {
		addr = strings.TrimSpace(lines[1])
	}
	return pid, addr
}

func Start(cfg config.Config) (*Daemon, error) {
	release, err := lock(cfg)
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
	d := &Daemon{cfg: cfg, app: app, server: rpc.New(app), release: release, done: make(chan struct{})}
	d.server.OnShutdown = d.requestStop
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

// Done is closed once the daemon has been asked to stop over RPC (or has
// stopped). Whoever runs the daemon then calls Stop, as for a signal.
func (d *Daemon) Done() <-chan struct{} { return d.done }

func (d *Daemon) requestStop() { d.doneOnce.Do(func() { close(d.done) }) }

// Stop stops serving, closes the core (goals, runs, MCP servers, terminals,
// store) and lets go of the data dir. It may be called more than once.
func (d *Daemon) Stop() error {
	d.stopOnce.Do(func() {
		d.requestStop()
		_ = d.server.Close()
		d.stopErr = d.app.Close()
		d.wg.Wait()
		if d.release != nil {
			d.release()
		}
	})
	return d.stopErr
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
