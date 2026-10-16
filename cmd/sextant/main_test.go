package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestDesktopMissingAppExits(t *testing.T) {
	if os.Getenv("ROVECODE_TEST_DESKTOP_MISS") == "1" {
		if !launchDesktop() {
			desktopMissing()
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestDesktopMissingAppExits")
	cmd.Env = append(os.Environ(), "ROVECODE_TEST_DESKTOP_MISS=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected desktop miss to fail, got %s", out)
	}
	if !strings.Contains(string(out), "desktop app is not installed") {
		t.Fatalf("missing desktop error: %s", out)
	}
	// macOS says how to install the app; elsewhere there is none, and the
	// way to the terminal UI is given instead
	hint := "install-desktop.sh"
	if runtime.GOOS != "darwin" {
		hint = "rovecode tui"
	}
	if !strings.Contains(string(out), hint) {
		t.Fatalf("missing hint %q: %s", hint, out)
	}
}
