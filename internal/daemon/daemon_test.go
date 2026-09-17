package daemon

import (
	"net/http"
	"testing"
	"time"

	"github.com/aether-dev/aether/internal/config"
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
	if d.App() == nil || d.App().Token == "" {
		t.Fatal("missing core")
	}
	_ = time.Second
}
