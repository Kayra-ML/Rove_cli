//go:build !windows

package tool

import (
	"os/exec"
	"syscall"
	"time"
)

func shellBin() string { return "/bin/sh" }
func shellArg() string { return "-c" }

// shellWaitDelay is how long a finished command's background children may
// hold its output open before the call returns without them.
const shellWaitDelay = 3 * time.Second

// killGroup runs the command in a process group of its own, so stopping it
// stops what it started too.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
