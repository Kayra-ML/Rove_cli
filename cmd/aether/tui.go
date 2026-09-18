package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// launchTUI starts the Rove Code cockpit. The shipped product binary is
// `rovecode`; this CLI remains a thin alias during the rename.
func launchTUI(args []string) {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rovecode: %v\n", err)
		os.Exit(1)
	}
	dir := filepath.Dir(self)
	candidates := []string{
		filepath.Join(dir, "rovecode"),
		filepath.Join(dir, "sextant"),
		"rovecode",
		"sextant",
	}
	for _, bin := range candidates {
		if filepath.IsAbs(bin) {
			if _, err := os.Stat(bin); err != nil {
				continue
			}
		} else if _, err := exec.LookPath(bin); err != nil {
			continue
		}
		cmd := exec.Command(bin, args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "rovecode: cockpit binary not found")
	os.Exit(1)
}
