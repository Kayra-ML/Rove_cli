//go:build !windows

package tool

func shellBin() string { return "/bin/sh" }
func shellArg() string { return "-c" }
