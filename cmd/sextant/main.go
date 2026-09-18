package main

import (
	"fmt"
	"os"

	"github.com/aether-dev/aether/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
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