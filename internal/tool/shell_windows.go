//go:build windows

package tool

func shellBin() string { return "cmd.exe" }
func shellArg() string { return "/C" }
