package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/aether-dev/aether/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Determine socket path (matches daemon default)
	home, _ := os.UserHomeDir()
	socketPath := filepath.Join(home, ".local", "share", "aether", "aether.sock")

	// If socket doesn't exist, try to start the daemon
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		fmt.Println("Starting Aether daemon...")
		cmd := exec.Command("aether", "daemon", "--background")
		if startErr := cmd.Start(); startErr != nil {
			// aether binary not in PATH or failed — warn but continue
			fmt.Fprintf(os.Stderr, "sextant: daemon not running (could not start: %v)\n", startErr)
		} else {
			// Wait for socket to appear
			time.Sleep(1500 * time.Millisecond)
		}
	}

	ipc, err := tui.NewIPCClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sextant: failed to build IPC client: %v\n", err)
		os.Exit(1)
	}

	m := tui.NewModel(ipc)

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
}