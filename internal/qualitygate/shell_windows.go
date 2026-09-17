//go:build windows

package qualitygate

func shell() string     { return "cmd.exe" }
func shellFlag() string { return "/C" }
