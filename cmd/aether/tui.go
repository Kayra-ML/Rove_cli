package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// launchTUI starts the sextant TUI by exec-ing the sextant binary.
// This allows `aether` (with no args) to be a convenient alias for `sextant`.
func launchTUI(args []string) {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aether: %v\n", err)
		os.Exit(1)
	}
	sextantBin := filepath.Join(filepath.Dir(self), "sextant")
	if _, err := os.Stat(sextantBin); err != nil {
		// Fall back to PATH
		sextantBin = "sextant"
	}
	cmd := exec.Command(sextantBin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}