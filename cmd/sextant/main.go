package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aether-dev/aether/internal/sshtunnel"
	"github.com/aether-dev/aether/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

var (
	flagHost      = flag.String("host", "", "Remote host to connect to (user@host:port or saved alias)")
	flagAddHost   = flag.String("add-host", "", "Add a saved host alias: alias=user@host:port[:note]")
	flagListHosts = flag.Bool("list-hosts", false, "List saved SSH hosts and exit")
)

func main() {
	flag.Parse()

	// --list-hosts: print and exit
	if *flagListHosts {
		store, err := sshtunnel.LoadHosts()
		if err != nil {
			fmt.Fprintf(os.Stderr, "sextant: %v\n", err)
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
			fmt.Fprintf(os.Stderr, "sextant: --add-host format: alias=user@host:port\n")
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
			fmt.Fprintf(os.Stderr, "sextant: add-host: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("saved host: %s → %s\n", alias, spec)
		return
	}

	// Determine socket path (matches daemon default)
	home, _ := os.UserHomeDir()
	socketPath := filepath.Join(home, ".local", "share", "aether", "aether.sock")

	var ipc *tui.IPCClient
	var tunnel *sshtunnel.Tunnel

	if *flagHost != "" {
		// SSH tunnel mode — resolve alias or parse spec
		spec := *flagHost
		store, _ := sshtunnel.LoadHosts()
		if store != nil {
			if saved, ok := store.Get(*flagHost); ok {
				spec = saved.Spec
				fmt.Fprintf(os.Stderr, "sextant: connecting to %s (%s) via SSH tunnel…\n", saved.Alias, spec)
			}
		}
		t, err := sshtunnel.NewTunnel(spec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sextant: invalid host spec: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "sextant: opening SSH tunnel to %s…\n", spec)
		if err := t.Open(); err != nil {
			fmt.Fprintf(os.Stderr, "sextant: SSH tunnel failed: %v\n", err)
			os.Exit(1)
		}
		tunnel = t
		tok, _ := t.FetchRemoteToken()
		ipc = tui.NewIPCClientTCP(t.LocalAddr(), tok)
	} else {
		// Local mode — start daemon if not running
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			fmt.Println("Starting Aether daemon...")
			cmd := exec.Command("aether", "daemon", "--background")
			if startErr := cmd.Start(); startErr != nil {
				fmt.Fprintf(os.Stderr, "sextant: daemon not running (could not start: %v)\n", startErr)
			} else {
				time.Sleep(1500 * time.Millisecond)
			}
		}

		var err error
		ipc, err = tui.NewIPCClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "sextant: failed to build IPC client: %v\n", err)
			os.Exit(1)
		}
	}

	m := tui.NewModel(ipc)

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
		fmt.Fprintf(os.Stderr, "sextant: %v\n", err)
		os.Exit(1)
	}

	// Clean up tunnel on exit
	if tunnel != nil {
		tunnel.Close()
	}
}