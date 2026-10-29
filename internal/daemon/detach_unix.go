//go:build !windows

package daemon

import (
	"errors"
	"os/exec"
	"syscall"
)

// Detach makes cmd a daemon of its own: a session apart from the terminal
// it was started from, so closing that terminal (its SIGHUP) does not take
// the daemon down with it.
func Detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}

// Alive reports whether a process with this pid is running.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
