//go:build windows

package provider

import "os/exec"

// killTree is the default on Windows: CommandContext kills the process.
func killTree(cmd *exec.Cmd) {}
