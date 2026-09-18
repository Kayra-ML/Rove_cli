package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDesktopMissingAppExits(t *testing.T) {
	if os.Getenv("ROVECODE_TEST_DESKTOP_MISS") == "1" {
		launchDesktop()
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
}
