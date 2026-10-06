package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// local is a Transport to a pretend server on this machine: scripts run in
// sh with HOME set to a temp dir, and forwards reach a test HTTP server
// standing in for the daemon.
type local struct {
	home   string
	daemon string // host:port of the stand-in daemon
	mu     sync.Mutex
	fwds   []*tcpForward
	execs  []string
}

func (l *local) Exec(ctx context.Context, script string, stdin io.Reader) (string, error) {
	l.mu.Lock()
	l.execs = append(l.execs, script)
	l.mu.Unlock()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Env = []string{"HOME=" + l.home, "PATH=/usr/bin:/bin"}
	cmd.Stdin = stdin
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%v: %s", err, errb.String())
	}
	return out.String(), nil
}

func (l *local) Forward(localPort, _ int) (Forward, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		return nil, err
	}
	f := &tcpForward{ln: ln, done: make(chan struct{})}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				f.once.Do(func() { close(f.done) })
				return
			}
			go func() {
				d, err := net.Dial("tcp", l.daemon)
				if err != nil {
					c.Close()
					return
				}
				go func() { _, _ = io.Copy(d, c); d.Close() }()
				_, _ = io.Copy(c, d)
				c.Close()
			}()
		}
	}()
	l.mu.Lock()
	l.fwds = append(l.fwds, f)
	l.mu.Unlock()
	return f, nil
}

func (l *local) last() *tcpForward {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fwds[len(l.fwds)-1]
}

type tcpForward struct {
	ln   net.Listener
	done chan struct{}
	once sync.Once
}

func (f *tcpForward) Done() <-chan struct{} { return f.done }
func (f *tcpForward) Close()                { _ = f.ln.Close(); <-f.done }

// fakeRovecode is the "binary" the app installs: a script that knows the
// commands the connection uses.
func fakeRovecode(level int) string {
	return fmt.Sprintf(`#!/bin/sh
case "$1" in
  api-level) echo %d ;;
  token) [ -f "$HOME/tok" ] && cat "$HOME/tok" || exit 1 ;;
  daemon) echo tok-123 > "$HOME/tok"; echo run >> "$HOME/daemon-runs" ;;
esac
`, level)
}

func gz(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

func standIn(t *testing.T, level *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"ok":true,"apiLevel":%d}`, atomic.LoadInt32(level))
	}))
	t.Cleanup(srv.Close)
	return srv
}

var onlyHome = []string{`"$HOME/.local/bin/rovecode"`}

func TestConnectInstallsStartsAndTunnels(t *testing.T) {
	level := int32(12)
	srv := standIn(t, &level)
	home := t.TempDir()
	tr := &local{home: home, daemon: strings.TrimPrefix(srv.URL, "http://")}
	var asked []string
	bins := func(goos, goarch string) ([]byte, error) {
		asked = append(asked, goos+"/"+goarch)
		return gz(fakeRovecode(12)), nil
	}
	var steps []string
	c, err := Dial(context.Background(), Target{Host: "srv"}, tr, bins, 12, Options{Search: onlyHome, Progress: func(s string) { steps = append(steps, s) }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	st := c.State()
	if st.Status != "connected" || st.Token != "tok-123" || !st.Installed || st.Host != "srv" {
		t.Fatalf("state = %+v", st)
	}
	if len(asked) != 1 || !strings.HasSuffix(asked[0], "/amd64") && !strings.HasSuffix(asked[0], "/arm64") {
		t.Fatalf("asked for %v", asked)
	}
	if fi, err := os.Stat(filepath.Join(home, ".local/bin/rovecode")); err != nil || fi.Mode()&0o100 == 0 {
		t.Fatalf("not installed: %v", err)
	}
	// the daemon answers through the tunnel
	resp, err := http.Get(st.HTTP + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if strings.Join(steps, ",") != "checking the server,installing rovecode on the server,starting Rove on the server,opening the tunnel" {
		t.Fatalf("steps = %v", steps)
	}

	// a second connect finds it installed and does not install again
	c2, err := Dial(context.Background(), Target{Host: "srv"}, tr, func(string, string) ([]byte, error) {
		return nil, errors.New("must not be asked")
	}, 12, Options{Search: onlyHome})
	if err != nil {
		t.Fatal(err)
	}
	if c2.State().Installed {
		t.Fatal("installed twice")
	}
	c2.Close()
}

func TestOlderRovecodeIsReplaced(t *testing.T) {
	level := int32(12)
	srv := standIn(t, &level)
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".local/bin"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".local/bin/rovecode"), []byte(fakeRovecode(3)), 0o755)
	tr := &local{home: home, daemon: strings.TrimPrefix(srv.URL, "http://")}
	c, err := Dial(context.Background(), Target{Host: "srv"}, tr, func(string, string) ([]byte, error) { return gz(fakeRovecode(12)), nil }, 12, Options{Search: onlyHome})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.State().Installed {
		t.Fatal("old rovecode kept")
	}
	b, _ := os.ReadFile(filepath.Join(home, ".local/bin/rovecode"))
	if !strings.Contains(string(b), "echo 12") {
		t.Fatal("binary not replaced")
	}
	// the old daemon was asked to stop — by its own path only
	tr.mu.Lock()
	install := tr.execs[1]
	tr.mu.Unlock()
	if !strings.Contains(install, "pkill -u \"$(id -u)\" -f '"+filepath.Join(home, ".local/bin/rovecode")+" daemon'") {
		t.Fatalf("install script = %s", install)
	}
}

func TestReconnectsWhenTheTunnelDrops(t *testing.T) {
	level := int32(12)
	srv := standIn(t, &level)
	tr := &local{home: t.TempDir(), daemon: strings.TrimPrefix(srv.URL, "http://")}
	var mu sync.Mutex
	var seen []string
	c, err := Dial(context.Background(), Target{Host: "srv"}, tr, func(string, string) ([]byte, error) { return gz(fakeRovecode(12)), nil }, 12, Options{
		Search:     onlyHome,
		CheckEvery: 50 * time.Millisecond,
		OnState: func(s State) {
			mu.Lock()
			seen = append(seen, s.Status)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	first := c.State().HTTP
	tr.last().Close() // the tunnel drops
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (c.State().Status != "connected" || c.State().HTTP == first) {
		time.Sleep(20 * time.Millisecond)
	}
	if st := c.State(); st.Status != "connected" || st.HTTP == first {
		t.Fatalf("did not reconnect: %+v", st)
	}
	mu.Lock()
	got := strings.Join(seen, ",")
	mu.Unlock()
	if !strings.Contains(got, "connected,reconnecting") {
		t.Fatalf("states = %s", got)
	}
	if _, err := http.Get(c.State().HTTP + "/health"); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if c.State().Status != "local" {
		t.Fatalf("after close = %+v", c.State())
	}
}

func TestConnectFailures(t *testing.T) {
	level := int32(12)
	srv := standIn(t, &level)
	tr := &local{home: t.TempDir(), daemon: strings.TrimPrefix(srv.URL, "http://")}
	// nothing on the server and no build to install
	_, err := Dial(context.Background(), Target{Host: "srv"}, tr, func(goos, goarch string) ([]byte, error) {
		return nil, fmt.Errorf("none for %s/%s", goos, goarch)
	}, 12, Options{Search: onlyHome})
	if err == nil || !strings.Contains(err.Error(), "rovecode is not on the server") {
		t.Fatalf("no build = %v", err)
	}
	// a daemon already running on the server that is too old
	atomic.StoreInt32(&level, 3)
	_, err = Dial(context.Background(), Target{Host: "srv"}, tr, func(string, string) ([]byte, error) { return gz(fakeRovecode(12)), nil }, 12, Options{Search: onlyHome, ReadyWithin: time.Second})
	if err == nil || !strings.Contains(err.Error(), "older (API 3)") {
		t.Fatalf("old daemon = %v", err)
	}
}

func TestProbeAndTargets(t *testing.T) {
	p := parseProbe("os=Linux\narch=aarch64\nbin=/home/u/.local/bin/rovecode\nlevel=12\n")
	if p.OS != "Linux" || p.Arch != "aarch64" || p.Level != 12 {
		t.Fatalf("probe = %+v", p)
	}
	for in, want := range map[[2]string]string{{"Linux", "x86_64"}: "linux/amd64", {"Linux", "aarch64"}: "linux/arm64", {"Darwin", "arm64"}: "darwin/arm64"} {
		goos, goarch, err := goTarget(in[0], in[1])
		if err != nil || goos+"/"+goarch != want {
			t.Fatalf("%v = %s/%s %v", in, goos, goarch, err)
		}
	}
	if _, _, err := goTarget("FreeBSD", "x86_64"); err == nil {
		t.Fatal("unsupported OS accepted")
	}
	if shq("it's") != `'it'\''s'` {
		t.Fatal(shq("it's"))
	}
	if (SSH{Target: Target{Host: "prod"}}).args()[0] != "-o" {
		t.Fatal("args")
	}
	for _, a := range (SSH{Target: Target{Host: "prod"}}).args() {
		if a == "-p" {
			t.Fatal("a config alias must keep its own port")
		}
	}
}
