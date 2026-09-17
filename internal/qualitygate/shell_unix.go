//go:build !windows

package qualitygate

func shell() string     { return "/bin/sh" }
func shellFlag() string { return "-c" }
