package agent

import (
	"strings"
	"testing"
)

func TestDescribeEnv(t *testing.T) {
	fake := func(name string, args ...string) string {
		switch name {
		case "sw_vers":
			return "26.1"
		case "python3":
			if len(args) > 0 && args[0] == "-c" {
				return "numpy requests"
			}
			return "Python 3.13.2"
		case "go":
			return "go version go1.25.0 darwin/arm64"
		}
		return ""
	}
	s := describeEnv(fake)
	for _, want := range []string{"/bin/sh", "never PowerShell", "python3 (3.13.2)", "go (1.25.0)", "python3 has: numpy requests", "lacks: pandas matplotlib scipy sklearn", "do not run it again unchanged"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "node (") {
		t.Errorf("node listed though not installed:\n%s", s)
	}
}

func TestRepeatNote(t *testing.T) {
	failed := map[string]string{}
	k := "shell\x00{\"command\":\"python3 x.py\"}"
	if out := repeatNote(failed, k, "ModuleNotFoundError", true); strings.Contains(out, "[rove]") {
		t.Fatal("first failure marked")
	}
	if out := repeatNote(failed, k, "ModuleNotFoundError", true); !strings.Contains(out, "[rove]") {
		t.Fatal("repeat not marked")
	}
	if out := repeatNote(failed, k, "other error", true); strings.Contains(out, "[rove]") {
		t.Fatal("a different failure was marked")
	}
	repeatNote(failed, k, "ok", false)
	if _, ok := failed[k]; ok {
		t.Fatal("success did not clear")
	}
}
