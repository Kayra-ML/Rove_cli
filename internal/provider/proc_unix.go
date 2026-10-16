//go:build !windows

package provider

import (
	"os/exec"
	"syscall"
)

// killTree runs an agent system in a process group of its own, so stopping
// a run stops what it started too (the commands and servers it ran), not
// only the CLI itself.
func killTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
