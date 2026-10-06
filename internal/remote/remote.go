// Package remote runs Rove on a server and connects this computer to it
// over SSH: it installs (or updates) rovecode there when needed, starts the
// daemon, reads its token, forwards its port to this machine, and keeps
// that forward alive. Everything — model calls, tools, files, terminals —
// then happens on the server; this computer only shows it.
package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DaemonPort is where the daemon listens on the server (loopback only).
const DaemonPort = 7420

// Target is an SSH server: an address, or a Host alias of ~/.ssh/config.
type Target struct {
	Host    string `json:"host"`
	User    string `json:"user,omitempty"`
	Port    int    `json:"port,omitempty"`
	KeyPath string `json:"keyPath,omitempty"`
}

// Label names the target for people ("deploy@prod").
func (t Target) Label() string {
	if t.User != "" {
		return t.User + "@" + t.Host
	}
	return t.Host
}

// Transport reaches the server: it runs scripts there and forwards a local
// port to one of its ports.
type Transport interface {
	Exec(ctx context.Context, script string, stdin io.Reader) (string, error)
	Forward(localPort, remotePort int) (Forward, error)
}

// Forward is a running port forward.
type Forward interface {
	Done() <-chan struct{}
	Close()
}

// Binaries returns rovecode for a server's OS and CPU ("linux", "arm64"),
// gzip-compressed, or an error when this app carries none for it.
type Binaries func(goos, goarch string) ([]byte, error)

// State is what the app shows about the connection.
type State struct {
	Status    string `json:"status"` // local | connecting | connected | reconnecting | error
	Host      string `json:"host,omitempty"`
	HTTP      string `json:"http,omitempty"` // http://127.0.0.1:port — the daemon, through the tunnel
	Token     string `json:"token,omitempty"`
	Message   string `json:"message,omitempty"`
	Installed bool   `json:"installed,omitempty"` // rovecode was installed or updated on the server
}

// Conn is a live connection to a server's daemon.
type Conn struct {
	target   Target
	tr       Transport
	bins     Binaries
	apiLevel int
	onState  func(State)
	client   *http.Client
	search   []string

	mu    sync.Mutex
	state State
	fwd   Forward
	stop  chan struct{}
	done  chan struct{}
}

// Options tune a connection (tests shorten the timings).
type Options struct {
	// Progress reports what is happening while connecting.
	Progress func(string)
	// OnState is called on every state change, from any goroutine.
	OnState func(State)
	// CheckEvery is how often the connection is checked (default 5s).
	CheckEvery time.Duration
	// ReadyWithin bounds the wait for the daemon behind a new forward.
	ReadyWithin time.Duration
	// Search is where rovecode is looked for on the server, in order
	// (shell words; default: ~/.local/bin, the PATH, /usr/local/bin).
	Search []string
}

var defaultSearch = []string{`"$HOME/.local/bin/rovecode"`, `"$(command -v rovecode 2>/dev/null)"`, `/usr/local/bin/rovecode`}

// Dial connects to the daemon on the target, setting it up first when
// needed. apiLevel is what this app needs; an older rovecode on the server
// is replaced.
func Dial(ctx context.Context, t Target, tr Transport, bins Binaries, apiLevel int, opt Options) (*Conn, error) {
	c := &Conn{
		target: t, tr: tr, bins: bins, apiLevel: apiLevel, onState: opt.OnState,
		client: &http.Client{Timeout: 3 * time.Second},
		stop:   make(chan struct{}), done: make(chan struct{}),
		search: opt.Search,
	}
	if len(c.search) == 0 {
		c.search = defaultSearch
	}
	if opt.CheckEvery <= 0 {
		opt.CheckEvery = 5 * time.Second
	}
	if opt.ReadyWithin <= 0 {
		opt.ReadyWithin = 12 * time.Second
	}
	progress := opt.Progress
	if progress == nil {
		progress = func(string) {}
	}
	c.set(State{Status: "connecting", Host: t.Label()})
	st, err := c.setup(ctx, progress, opt.ReadyWithin)
	if err != nil {
		c.set(State{Status: "error", Host: t.Label(), Message: err.Error()})
		return nil, err
	}
	c.set(st)
	go c.watch(opt.CheckEvery, opt.ReadyWithin)
	return c, nil
}

// State is the connection's current state.
func (c *Conn) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Close ends the connection: the watcher stops and the forward closes. The
// daemon keeps running on the server.
func (c *Conn) Close() {
	select {
	case <-c.stop:
		return
	default:
		close(c.stop)
	}
	<-c.done
	c.mu.Lock()
	if c.fwd != nil {
		c.fwd.Close()
		c.fwd = nil
	}
	c.mu.Unlock()
	c.set(State{Status: "local"})
}

func (c *Conn) set(s State) {
	c.mu.Lock()
	c.state = s
	c.mu.Unlock()
	if c.onState != nil {
		c.onState(s)
	}
}

// probe is what the server has: its OS and CPU, a rovecode (the one in
// ~/.local/bin first), and that rovecode's API level.
type probe struct {
	OS, Arch, Bin string
	Level         int
}

func probeScript(search []string) string {
	return `OS=$(uname -s); ARCH=$(uname -m); BIN=""
for b in ` + strings.Join(search, " ") + `; do
  if [ -n "$b" ] && [ -x "$b" ]; then BIN="$b"; break; fi
done
LVL=0
if [ -n "$BIN" ]; then LVL=$("$BIN" api-level 2>/dev/null || echo 0); fi
echo "os=$OS"; echo "arch=$ARCH"; echo "bin=$BIN"; echo "level=$LVL"`
}

func parseProbe(out string) probe {
	var p probe
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "os":
			p.OS = v
		case "arch":
			p.Arch = v
		case "bin":
			p.Bin = v
		case "level":
			p.Level, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	return p
}

// goTarget maps uname to Go's names ("Linux", "x86_64" → "linux", "amd64").
func goTarget(osName, arch string) (string, string, error) {
	var goos, goarch string
	switch strings.ToLower(osName) {
	case "linux":
		goos = "linux"
	case "darwin":
		goos = "darwin"
	default:
		return "", "", fmt.Errorf("unsupported server OS %q", osName)
	}
	switch strings.ToLower(arch) {
	case "x86_64", "amd64":
		goarch = "amd64"
	case "aarch64", "arm64":
		goarch = "arm64"
	default:
		return "", "", fmt.Errorf("unsupported server CPU %q", arch)
	}
	return goos, goarch, nil
}

// shq quotes a string for a POSIX shell.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// setup makes the daemon reachable: installs or updates rovecode, starts
// the daemon, reads the token and opens the forward.
func (c *Conn) setup(ctx context.Context, progress func(string), readyWithin time.Duration) (State, error) {
	st := State{Status: "connected", Host: c.target.Label()}
	progress("checking the server")
	out, err := c.tr.Exec(ctx, probeScript(c.search), nil)
	if err != nil {
		return st, fmt.Errorf("cannot reach %s over SSH: %w", c.target.Label(), err)
	}
	p := parseProbe(out)
	bin := p.Bin
	if bin == "" || p.Level < c.apiLevel {
		goos, goarch, err := goTarget(p.OS, p.Arch)
		if err != nil {
			return st, err
		}
		gz, err := c.bins(goos, goarch)
		if err != nil {
			return st, fmt.Errorf("rovecode is not on the server and this app has no %s/%s build to install: %w", goos, goarch, err)
		}
		raw, err := gunzip(gz)
		if err != nil {
			return st, err
		}
		if bin == "" {
			progress("installing rovecode on the server")
		} else {
			progress("updating rovecode on the server")
		}
		// an older daemon must go before the new one starts
		stopOld := ""
		if bin != "" {
			stopOld = "pkill -u \"$(id -u)\" -f " + shq(bin+" daemon") + " 2>/dev/null || true; sleep 1; "
		}
		script := stopOld + `set -e; mkdir -p "$HOME/.local/bin"
cat > "$HOME/.local/bin/rovecode.new"; chmod 0755 "$HOME/.local/bin/rovecode.new"
mv "$HOME/.local/bin/rovecode.new" "$HOME/.local/bin/rovecode"
"$HOME/.local/bin/rovecode" api-level`
		lvl, err := c.tr.Exec(ctx, script, bytes.NewReader(raw))
		if err != nil {
			return st, fmt.Errorf("installing rovecode on the server failed: %w", err)
		}
		if n, _ := strconv.Atoi(strings.TrimSpace(lvl)); n < c.apiLevel {
			return st, fmt.Errorf("the rovecode installed on the server is too old (API %d, need %d)", n, c.apiLevel)
		}
		bin = "$HOME/.local/bin/rovecode"
		st.Installed = true
	}

	progress("starting Rove on the server")
	// A daemon already running keeps its lock; a second one just exits.
	binRef := shq(bin)
	if strings.HasPrefix(bin, "$HOME") {
		binRef = `"` + bin + `"`
	}
	start := `B=` + binRef + `
mkdir -p "$HOME/.cache"
nohup "$B" daemon >>"$HOME/.cache/rovecode-daemon.log" 2>&1 </dev/null &
i=0; while [ $i -lt 50 ]; do
  T=$("$B" token 2>/dev/null) && [ -n "$T" ] && { echo "$T"; exit 0; }
  i=$((i+1)); sleep 0.2
done
echo "the daemon did not start; see ~/.cache/rovecode-daemon.log" >&2; exit 1`
	tok, err := c.tr.Exec(ctx, start, nil)
	if err != nil {
		return st, fmt.Errorf("starting Rove on the server failed: %w", err)
	}
	st.Token = strings.TrimSpace(lastLine(tok))

	progress("opening the tunnel")
	port, err := freePort()
	if err != nil {
		return st, err
	}
	fwd, err := c.tr.Forward(port, DaemonPort)
	if err != nil {
		return st, fmt.Errorf("opening the SSH tunnel failed: %w", err)
	}
	st.HTTP = fmt.Sprintf("http://127.0.0.1:%d", port)
	level, err := c.waitHealthy(st.HTTP, fwd, readyWithin)
	if err != nil {
		fwd.Close()
		return st, err
	}
	if level < c.apiLevel {
		fwd.Close()
		return st, fmt.Errorf("the daemon on the server is older (API %d) than this app needs (%d); restart it: pkill -f 'rovecode daemon'", level, c.apiLevel)
	}
	c.mu.Lock()
	if c.fwd != nil {
		c.fwd.Close()
	}
	c.fwd = fwd
	c.mu.Unlock()
	return st, nil
}

// waitHealthy polls the daemon through the forward until it answers.
func (c *Conn) waitHealthy(base string, fwd Forward, within time.Duration) (int, error) {
	deadline := time.Now().Add(within)
	var last error
	for time.Now().Before(deadline) {
		select {
		case <-fwd.Done():
			return 0, errors.New("the SSH tunnel closed")
		default:
		}
		level, err := c.health(base)
		if err == nil {
			return level, nil
		}
		last = err
		time.Sleep(150 * time.Millisecond)
	}
	return 0, fmt.Errorf("the daemon on the server did not answer: %v", last)
}

func (c *Conn) health(base string) (int, error) {
	resp, err := c.client.Get(base + "/health")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var h struct {
		OK       bool `json:"ok"`
		APILevel int  `json:"apiLevel"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil || !h.OK {
		return 0, fmt.Errorf("unhealthy daemon")
	}
	return h.APILevel, nil
}

// watch checks the connection and sets it up again when it drops: a closed
// tunnel, or a daemon that stops answering twice in a row.
func (c *Conn) watch(every, readyWithin time.Duration) {
	defer close(c.done)
	tick := time.NewTicker(every)
	defer tick.Stop()
	misses := 0
	for {
		c.mu.Lock()
		fwd, base := c.fwd, c.state.HTTP
		c.mu.Unlock()
		var dropped <-chan struct{}
		if fwd != nil {
			dropped = fwd.Done()
		}
		select {
		case <-c.stop:
			return
		case <-dropped:
			misses = 2
		case <-tick.C:
			if _, err := c.health(base); err != nil {
				misses++
			} else {
				misses = 0
			}
		}
		if misses < 2 {
			continue
		}
		misses = 0
		c.reconnect(readyWithin)
	}
}

// reconnect tries again with growing pauses until it works or is closed.
func (c *Conn) reconnect(readyWithin time.Duration) {
	wait := 500 * time.Millisecond
	for attempt := 1; ; attempt++ {
		prev := c.State()
		c.set(State{Status: "reconnecting", Host: c.target.Label(), HTTP: prev.HTTP, Token: prev.Token, Message: fmt.Sprintf("attempt %d", attempt)})
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		st, err := c.setup(ctx, func(string) {}, readyWithin)
		cancel()
		if err == nil {
			c.set(st)
			return
		}
		select {
		case <-c.stop:
			return
		case <-time.After(wait):
		}
		if wait < 30*time.Second {
			wait *= 2
		}
	}
}

func gunzip(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("bundled rovecode is damaged: %w", err)
	}
	defer r.Close()
	return io.ReadAll(r)
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
