//go:build windows

package tool

import (
	"os/exec"
	"time"
)

func shellBin() string { return "cmd.exe" }
func shellArg() string { return "/C" }

// shellWaitDelay is how long a finished command's background children may
// hold its output open before the call returns without them.
const shellWaitDelay = 3 * time.Second

// killGroup is the default on Windows: CommandContext kills the process.
func killGroup(cmd *exec.Cmd) {}
