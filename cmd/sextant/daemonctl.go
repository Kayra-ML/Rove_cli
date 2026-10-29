package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/daemon"
	"github.com/Kayra-ML/rove/pkg/client"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// rovecode daemon [run|start|stop|restart|status|logs]: the daemon in the
// foreground (run, the default — what the app and servers start), or the
// background one looked after from a terminal.

// exitNotRunning is status's answer for a daemon that is not running, as
// init scripts and systemd read it.
const exitNotRunning = 3

// stopWait bounds how long stop waits for the daemon to put things away.
var stopWait = 20 * time.Second

func daemonCommand(args []string) int {
	sub := "run"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	if sub == "run" {
		runBundledDaemon()
		return 0
	}
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "rovecode: config: %v\n", err)
		return 1
	}
	switch sub {
	case "start":
		return daemonStart(os.Stdout, cfg)
	case "stop":
		return daemonStop(os.Stdout, cfg)
	case "restart":
		if code := daemonStop(os.Stdout, cfg); code != 0 {
			return code
		}
		return daemonStart(os.Stdout, cfg)
	case "status":
		return daemonStatus(os.Stdout, cfg, len(args) > 0 && args[0] == "--json")
	case "logs":
		return daemonLogs(os.Stdout, cfg, args)
	case "help", "-h", "--help":
		printDaemonUsage(os.Stdout)
		return 0
	}
	fmt.Fprintf(os.Stderr, "rovecode daemon: unknown command %q\n", sub)
	printDaemonUsage(os.Stderr)
	return 2
}

func printDaemonUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: rovecode daemon [command]

  run              Run the daemon in the foreground (the default)
  start            Start it in the background, if it is not running
  stop             Stop it: running goals are left to resume on the next start
  restart          Stop it, then start it again (e.g. after an update)
  status [--json]  Whether it runs, and what it is doing (exit 3: not running)
  logs [-n N] [-f] Its log: the last N lines, and new ones as they come
`)
}

// health asks the daemon how it is, on its unauthenticated /health.
func health(addr string) (map[string]any, error) {
	c := &http.Client{Timeout: time.Second}
	resp, err := c.Get("http://" + addr + "/health")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health: %s", resp.Status)
	}
	var h map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, err
	}
	return h, nil
}

func daemonStart(w io.Writer, cfg config.Config) int {
	if daemonHealthy(cfg.ListenHTTP) {
		fmt.Fprintf(w, "rovecode daemon: already running on %s\n", cfg.ListenHTTP)
		return 0
	}
	if err := ensureBundledDaemon(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "rovecode daemon: %v\n", err)
		return 1
	}
	fmt.Fprintf(w, "rovecode daemon: started on %s\n", cfg.ListenHTTP)
	return 0
}

// daemonStop asks the daemon to stop (the shutdown method: goals are left
// to resume, MCP servers and shells end, the store is closed) and waits
// until it has; a daemon that does not take the call gets SIGTERM.
func daemonStop(w io.Writer, cfg config.Config) int {
	h, err := health(cfg.ListenHTTP)
	if err != nil {
		fmt.Fprintln(w, "rovecode daemon: not running")
		return 0
	}
	pid := intOf(h["pid"])
	if pid == 0 {
		pid = pidFromFile(cfg.DataDir)
	}
	cl, err := client.FromEnv()
	if err == nil {
		_, err = cl.Call(protocol.MethodShutdown, nil)
	}
	if err != nil {
		p, perr := os.FindProcess(pid)
		if pid == 0 || perr != nil {
			fmt.Fprintf(os.Stderr, "rovecode daemon: cannot ask it to stop: %v\n", err)
			return 1
		}
		if p.Signal(syscall.SIGTERM) != nil {
			_ = p.Kill() // no SIGTERM on windows
		}
	}
	if !waitStopped(cfg, pid, stopWait) {
		fmt.Fprintf(os.Stderr, "rovecode daemon: still stopping after %s; see `rovecode daemon logs`\n", stopWait)
		return 1
	}
	fmt.Fprintln(w, "rovecode daemon: stopped")
	return 0
}

// waitStopped waits for the daemon to stop answering, then for its process
// to end (a graceful stop goes on putting things away after that).
func waitStopped(cfg config.Config, pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !daemonHealthy(cfg.ListenHTTP) && (pid == 0 || !daemon.Alive(pid)) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func pidFromFile(dataDir string) int {
	b, err := os.ReadFile(daemon.PIDPath(dataDir))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func intOf(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

func daemonStatus(w io.Writer, cfg config.Config, asJSON bool) int {
	h, err := health(cfg.ListenHTTP)
	if err != nil {
		if asJSON {
			_ = json.NewEncoder(w).Encode(map[string]any{"running": false, "address": cfg.ListenHTTP})
		} else {
			fmt.Fprintf(w, "rovecode daemon: not running (%s)\n", cfg.ListenHTTP)
		}
		return exitNotRunning
	}
	h["running"] = true
	h["address"] = cfg.ListenHTTP
	h["log"] = daemon.LogPath(cfg.DataDir)
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(h)
		return 0
	}
	str := func(k string) string {
		switch v := h[k].(type) {
		case nil:
			return "-"
		case float64:
			return strconv.FormatInt(int64(v), 10)
		default:
			return fmt.Sprint(v)
		}
	}
	fmt.Fprintf(w, "rovecode daemon: running (pid %s, %s, api %s)\n", str("pid"), str("version"), str("apiLevel"))
	rows := [][2]string{
		{"address", cfg.ListenHTTP},
		{"uptime", str("uptime")},
		{"data", str("dataDir")},
		{"agents", str("activeAgents") + " running"},
		{"goals", str("goalsRunning") + " running"},
		{"terminals", str("terminals") + " open"},
		{"streams", str("streams") + " connected"},
		{"log", daemon.LogPath(cfg.DataDir)},
	}
	for _, r := range rows {
		fmt.Fprintf(w, "  %-10s %s\n", r[0], r[1])
	}
	if lvl := intOf(h["apiLevel"]); lvl > 0 && lvl < protocol.APILevel {
		fmt.Fprintf(w, "note: the daemon is older than this rovecode (api %d < %d); `rovecode daemon restart` starts this one\n", lvl, protocol.APILevel)
	}
	return 0
}

func daemonLogs(w io.Writer, cfg config.Config, args []string) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	n := fs.Int("n", 50, "how many of the last lines to show")
	follow := fs.Bool("f", false, "keep showing new lines")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	p := daemon.LogPath(cfg.DataDir)
	lines, size, err := tailLines(p, *n)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "rovecode daemon: no log yet (%s)\n", p)
			return 1
		}
		fmt.Fprintf(os.Stderr, "rovecode daemon: %v\n", err)
		return 1
	}
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
	if !*follow {
		return 0
	}
	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; close(stop) }()
	followFile(p, size, w, stop, 300*time.Millisecond)
	return 0
}

// tailLines is the last n lines of a file, and its size. The log is set
// aside at a few MB, so it is read whole.
func tailLines(path string, n int) ([]string, int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	text := strings.TrimRight(string(b), "\n")
	if text == "" || n <= 0 {
		return nil, int64(len(b)), nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, int64(len(b)), nil
}

// followFile writes what is added to path from offset on, until stop. A
// file that got shorter was set aside (rotated): it is read from its start.
func followFile(path string, offset int64, w io.Writer, stop <-chan struct{}, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		if fi.Size() < offset {
			offset = 0
		}
		if fi.Size() == offset {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err := f.Seek(offset, io.SeekStart); err == nil {
			var buf bytes.Buffer
			n, _ := io.Copy(&buf, f)
			offset += n
			_, _ = w.Write(buf.Bytes())
		}
		f.Close()
	}
}
