package daemon

import (
	"errors"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
)

func TestStartHealthAndStop(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		DataDir:    dir,
		ListenHTTP: "127.0.0.1:17420",
		ListenIPC:  dir + "/aether.sock",
	}
	d, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()
	resp, err := http.Get("http://127.0.0.1:17420/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if d.app == nil || d.app.Token == "" {
		t.Fatal("missing core")
	}
	_ = time.Second
}

// One data dir, one daemon: a second one would share the database but not
// the in-memory state, and steal the IPC socket from the first.
// shortSockDir keeps unix socket paths under macOS's 104-byte limit, which a
// test-named temp dir can exceed.
func shortSockDir(t *testing.T) string {
	d, err := os.MkdirTemp("/tmp", "rd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func TestSecondDaemonOnSameDataDirIsRefused(t *testing.T) {
	dir, socks := t.TempDir(), shortSockDir(t)
	cfg := config.Config{DataDir: dir, ListenHTTP: "127.0.0.1:17431", ListenIPC: socks + "/a.sock"}
	d, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dup := cfg
	dup.ListenHTTP = "127.0.0.1:17432"
	dup.ListenIPC = socks + "/b.sock"
	began := time.Now()
	if d2, err := Start(dup); !errors.Is(err, ErrAlreadyRunning) {
		if d2 != nil {
			d2.Stop()
		}
		t.Fatalf("second daemon: err = %v", err)
	}
	// the first one answers: it is running, not stopping, so no wait
	if time.Since(began) > 2*time.Second {
		t.Fatalf("refusing took %v", time.Since(began))
	}
	// the first keeps its socket
	if _, err := os.Stat(cfg.ListenIPC); err != nil {
		t.Fatalf("ipc socket gone: %v", err)
	}
	if err := d.Stop(); err != nil {
		t.Fatal(err)
	}
	// stopping releases the lock
	d3, err := Start(cfg)
	if err != nil {
		t.Fatalf("restart after stop: %v", err)
	}
	d3.Stop()
}

// A taken port is an error at start, not a daemon that looks healthy
// because someone else answers on that port.
func TestStartFailsWhenPortIsTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	dir, socks := t.TempDir(), shortSockDir(t)
	d, err := Start(config.Config{DataDir: dir, ListenHTTP: ln.Addr().String(), ListenIPC: socks + "/c.sock"})
	if err == nil {
		d.Stop()
		t.Fatal("started on a taken port")
	}
	// and it let go of the data dir
	d, err = Start(config.Config{DataDir: dir, ListenHTTP: "127.0.0.1:17433", ListenIPC: socks + "/c.sock"})
	if err != nil {
		t.Fatalf("data dir still locked after failed start: %v", err)
	}
	d.Stop()
}

// The shutdown method stops the daemon the way a signal does: Done closes,
// and Stop (safe to call twice) closes the core and lets go of the data dir.
func TestShutdownMethodStopsCleanly(t *testing.T) {
	dir, socks := t.TempDir(), shortSockDir(t)
	cfg := config.Config{DataDir: dir, ListenHTTP: "127.0.0.1:17434", ListenIPC: socks + "/s.sock"}
	d, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"method":"shutdown","token":"` + d.app.Token + `"}`
	resp, err := http.Post("http://"+cfg.ListenHTTP+"/rpc", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	select {
	case <-d.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the shutdown method did not reach the daemon")
	}
	if err := d.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := d.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if resp, err := http.Get("http://" + cfg.ListenHTTP + "/health"); err == nil {
		resp.Body.Close()
		t.Fatal("still serving after Stop")
	}
	d2, err := Start(cfg)
	if err != nil {
		t.Fatalf("start after a clean stop: %v", err)
	}
	d2.Stop()
}

// A daemon still putting things away holds the data dir without serving;
// one started meanwhile waits for it rather than failing.
func TestStartWaitsForAStoppingDaemon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no data dir lock on windows")
	}
	dir, socks := t.TempDir(), shortSockDir(t)
	release, err := lockDataDir(dir, "127.0.0.1:1") // a holder that serves nowhere
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(300 * time.Millisecond); release() }()
	began := time.Now()
	d, err := Start(config.Config{DataDir: dir, ListenHTTP: "127.0.0.1:17435", ListenIPC: socks + "/w.sock"})
	if err != nil {
		t.Fatalf("start while the old daemon stopped: %v", err)
	}
	d.Stop()
	if time.Since(began) < 300*time.Millisecond {
		t.Fatal("started without waiting for the data dir")
	}
}

// A daemon stopped the moment it has started still stops: its IPC listener
// may come up after Stop began, and must not keep it waiting.
func TestStopRightAfterStart(t *testing.T) {
	for i := 0; i < 5; i++ {
		dir, socks := t.TempDir(), shortSockDir(t)
		d, err := Start(config.Config{DataDir: dir, ListenHTTP: "127.0.0.1:17436", ListenIPC: socks + "/q.sock"})
		if err != nil {
			t.Fatal(err)
		}
		stopped := make(chan struct{})
		go func() { _ = d.Stop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Fatal("Stop hung right after Start")
		}
	}
}
