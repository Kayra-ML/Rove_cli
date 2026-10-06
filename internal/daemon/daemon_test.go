package daemon

import (
	"errors"
	"net"
	"net/http"
	"os"
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
	if d2, err := Start(dup); !errors.Is(err, ErrAlreadyRunning) {
		if d2 != nil {
			d2.Stop()
		}
		t.Fatalf("second daemon: err = %v", err)
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
