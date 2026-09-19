package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/daemon"
	"github.com/Kayra-ML/rove/internal/sshtunnel"
	"github.com/Kayra-ML/rove/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

var (
	version       = "dev"
	flagVersion   = flag.Bool("version", false, "Print the Rove Code version and exit")
	flagHost      = flag.String("host", "", "Remote host to connect to (user@host:port or saved alias)")
	flagAddHost   = flag.String("add-host", "", "Add a saved host alias: alias=user@host:port[:note]")
	flagListHosts = flag.Bool("list-hosts", false, "List saved SSH hosts and exit")
)

func main() {
	runAsTUI := false
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "daemon":
			runBundledDaemon()
			return
		case "desktop":
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
			launchDesktop()
			return
		case "tui":
			runAsTUI = true
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		case "help", "-h", "--help":
			printUsage()
			return
		case "version":
			fmt.Printf("rovecode %s\n", version)
			return
		}
	}

	flag.Parse()
	if *flagVersion {
		fmt.Printf("rovecode %s\n", version)
		return
	}

	// --list-hosts: print and exit
	if *flagListHosts {
		store, err := sshtunnel.LoadHosts()
		if err != nil {
			fmt.Fprintf(os.Stderr, "rovecode: %v\n", err)
			os.Exit(1)
		}
		if len(store.Hosts) == 0 {
			fmt.Println("(no saved hosts)")
			return
		}
		for _, h := range store.Hosts {
			last := "never"
			if !h.LastUsed.IsZero() {
				last = h.LastUsed.Format("2006-01-02 15:04")
			}
			note := ""
			if h.Note != "" {
				note = "  # " + h.Note
			}
			fmt.Printf("%-16s %s  (last: %s)%s\n", h.Alias, h.Spec, last, note)
		}
		return
	}

	// --add-host alias=user@host:port[:note]
	if *flagAddHost != "" {
		parts := strings.SplitN(*flagAddHost, "=", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "rovecode: --add-host format: alias=user@host:port\n")
			os.Exit(1)
		}
		alias := strings.TrimSpace(parts[0])
		rest := strings.TrimSpace(parts[1])
		spec := rest
		note := ""
		// Optional note after second '=' sign (alias=spec=note not supported; use colon in note)
		// Or allow alias=spec note
		if idx := strings.Index(rest, " "); idx > 0 {
			spec = rest[:idx]
			note = strings.TrimSpace(rest[idx+1:])
		}
		store, err := sshtunnel.LoadHosts()
		if err != nil {
			store = &sshtunnel.HostStore{}
		}
		if err := store.Add(alias, spec, note); err != nil {
			fmt.Fprintf(os.Stderr, "rovecode: add-host: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("saved host: %s → %s\n", alias, spec)
		return
	}

	if !runAsTUI {
		launchDesktop()
		return
	}

	runTUI()
}

func runTUI() {
	localConfig, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "rovecode: config: %v\n", err)
		os.Exit(1)
	}

	var ipc *tui.IPCClient
	var tunnel *sshtunnel.Tunnel
	var startupErr error

	if *flagHost != "" {
		// SSH tunnel mode — resolve alias or parse spec
		spec := *flagHost
		store, _ := sshtunnel.LoadHosts()
		if store != nil {
			if saved, ok := store.Get(*flagHost); ok {
				spec = saved.Spec
				fmt.Fprintf(os.Stderr, "rovecode: connecting to %s (%s) via SSH tunnel…\n", saved.Alias, spec)
			}
		}
		t, err := sshtunnel.NewTunnel(spec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rovecode: invalid host spec: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "rovecode: opening SSH tunnel to %s…\n", spec)
		if err := t.Open(); err != nil {
			fmt.Fprintf(os.Stderr, "rovecode: SSH tunnel failed: %v\n", err)
			os.Exit(1)
		}
		tunnel = t
		tok, _ := t.FetchRemoteToken()
		ipc = tui.NewIPCClientTCP(t.LocalAddr(), tok)
	} else {
		// Local mode — the installed rovecode binary also owns its daemon.
		startupErr = ensureBundledDaemon(localConfig)
		var err error
		ipc, err = tui.NewIPCClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "rovecode: IPC client: %v\n", err)
			os.Exit(1)
		}
	}

	// Show branding only for the interactive client, never the daemon child.
	showSplash()

	m := tui.NewModel(ipc)
	if startupErr != nil {
		m.SetStartupError(startupErr)
	}

	// If we opened a tunnel at startup, wire it into the SSH panel
	if tunnel != nil {
		alias := *flagHost
		store, _ := sshtunnel.LoadHosts()
		if store != nil {
			if saved, ok := store.Get(*flagHost); ok {
				alias = saved.Alias
				store.UpdateLastUsed(alias)
			}
		}
		m.SetInitialTunnel(tunnel, alias)
	}

	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Start SSE event subscription in the background.
	ipc.SubscribeEvents(p)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "rovecode: %v\n", err)
		os.Exit(1)
	}

	// Clean up tunnel on exit
	if tunnel != nil {
		tunnel.Close()
	}
}

func printUsage() {
	fmt.Print(`Rove Code — local-first coding agent

Usage:
  rovecode                 Open the desktop app
  rovecode desktop         Open the desktop app
  rovecode tui             Open the terminal cockpit
  rovecode daemon          Run the shared daemon in the foreground
  rovecode --version       Print the installed version
  rovecode tui --host alias
  rovecode --list-hosts    List saved SSH hosts
  rovecode --add-host alias=user@host:port

The desktop and daemon share the same sessions and workspace.
`)
}

func launchDesktop() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rovecode: %v\n", err)
		os.Exit(1)
	}
	dir := filepath.Dir(self)
	candidates := []string{
		filepath.Join(dir, "Rove Code.app", "Contents", "MacOS", "Rove Code"),
		filepath.Join(dir, "RoveCode.app", "Contents", "MacOS", "Rove Code"),
		filepath.Join(dir, "rovecode-desktop"),
		filepath.Join(dir, "aether-desktop"),
	}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			"/Applications/Rove Code.app/Contents/MacOS/Rove Code",
			"/Applications/RoveCode.app/Contents/MacOS/Rove Code",
		)
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			cmd := exec.Command(path)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = os.Environ()
			if err := cmd.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "rovecode: desktop: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("opened Rove Code desktop")
			return
		}
	}
	fmt.Fprintln(os.Stderr, "rovecode: desktop app is not installed yet.")
	fmt.Fprintln(os.Stderr, "On macOS, from any directory:")
	fmt.Fprintln(os.Stderr, "  curl -fsSL https://raw.githubusercontent.com/Kayra-ML/Rove_cli/main/install-desktop.sh | bash")
	os.Exit(1)
}

// showSplash renders the ROVE splash screen briefly, then clears.
func showSplash() {
	restore := func() { fmt.Print("\033[?25h") }
	defer restore()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	go func() {
		<-stop
		restore()
		os.Exit(130)
	}()
	fmt.Print("\033[?25l")
	// Clear screen, black bg
	fmt.Print("\033[2J\033[H")

	// ROVE ASCII art — large block letters, premium minimal style
	// Designed to echo the visual: dark bg, bold centered ROVE wordmark
	// "O" has a distinctive square-rounded feel matching the logo
	rove := []string{
		``,
		``,
		``,
		``,
		``,
		`  ██████╗   ██████╗  ██╗   ██╗ ███████╗`,
		`  ██╔══██╗ ██╔═══██╗ ██║   ██║ ██╔════╝`,
		`  ██████╔╝ ██║   ██║ ██║   ██║ █████╗  `,
		`  ██╔══██╗ ██║   ██║ ╚██╗ ██╔╝ ██╔══╝  `,
		`  ██║  ██║ ╚██████╔╝  ╚████╔╝  ███████╗`,
		`  ╚═╝  ╚═╝  ╚═════╝    ╚═══╝   ╚══════╝`,
		``,
		`              c o d e`,
		``,
	}

	// Get terminal width for centering
	cols := 80
	if c, err := getTermCols(); err == nil {
		cols = c
	}

	for _, line := range rove {
		pad := (cols - 42) / 2
		if pad < 0 {
			pad = 0
		}
		fmt.Printf("%*s%s\n", pad, "", line)
	}

	time.Sleep(700 * time.Millisecond)

	// Clear screen before TUI takes over
	fmt.Print("\033[2J\033[H")
	// Restore cursor (bubbletea will manage it from here)
	fmt.Print("\033[?25h")
}

func getTermCols() (int, error) {
	cmd := exec.Command("tput", "cols")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, b := range out {
		if b >= '0' && b <= '9' {
			n = n*10 + int(b-'0')
		}
	}
	if n == 0 {
		return 0, fmt.Errorf("no cols")
	}
	return n, nil
}

func daemonHealthy(addr string) bool {
	client := &http.Client{Timeout: 250 * time.Millisecond}
	resp, err := client.Get("http://" + addr + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func ensureBundledDaemon(cfg config.Config) error {
	if daemonHealthy(cfg.ListenHTTP) {
		return nil
	}
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(cfg.DataDir, "logs", "rovecode-daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "daemon")
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	_ = cmd.Process.Release()
	_ = logFile.Close()

	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		if daemonHealthy(cfg.ListenHTTP) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not become ready; see %s", logPath)
}

func runBundledDaemon() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := cfg.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if daemonHealthy(cfg.ListenHTTP) {
		return
	}
	d, err := daemon.Start(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = daemon.WritePID(cfg.DataDir)
	defer os.Remove(daemon.PIDPath(cfg.DataDir))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	_ = d.Stop()
}
