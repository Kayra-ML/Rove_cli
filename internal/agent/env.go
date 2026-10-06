package agent

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// environment tells the agent what computer it works on. Without it a model
// guesses: on a Mac it wrote PowerShell (`if ($?) { … }`) for the shell
// tool, and ran a python script again and again after it failed on a module
// that was not there. It is looked up once per daemon; the parts that can
// change (what is installed) are cheap to state and rarely do.
var envOnce sync.Once
var envBlock string

func environment() string {
	envOnce.Do(func() { envBlock = describeEnv(probe) })
	return envBlock
}

// probe runs a short command and returns its first line of output.
type prober func(name string, args ...string) string

func probe(name string, args ...string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	s := strings.TrimSpace(string(out))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// pyMods are the libraries scripts reach for most; saying which are there
// saves a run that fails on an import.
var pyMods = []string{"numpy", "pandas", "matplotlib", "scipy", "sklearn", "requests"}

func describeEnv(run prober) string {
	var b strings.Builder
	b.WriteString("\n\n## Environment\n")
	switch runtime.GOOS {
	case "darwin":
		v := run("sw_vers", "-productVersion")
		b.WriteString("OS: macOS " + v + " (" + runtime.GOARCH + ")\n")
	case "windows":
		b.WriteString("OS: Windows (" + runtime.GOARCH + ")\n")
	default:
		b.WriteString("OS: " + runtime.GOOS + " (" + runtime.GOARCH + ")\n")
	}
	if runtime.GOOS == "windows" {
		b.WriteString("Commands run with: cmd.exe /C\n")
	} else {
		b.WriteString("Commands run with: /bin/sh -c (POSIX sh syntax: `if cmd; then …; fi`, `&&`; never PowerShell)\n")
	}
	var have []string
	py := false
	for _, t := range [][]string{
		{"python3", "--version"}, {"node", "--version"}, {"go", "version"},
		{"git", "--version"}, {"brew", "--version"}, {"docker", "--version"},
	} {
		if v := run(t[0], t[1:]...); v != "" {
			have = append(have, t[0]+" ("+trimVersion(v)+")")
			py = py || t[0] == "python3"
		}
	}
	if len(have) > 0 {
		b.WriteString("Installed: " + strings.Join(have, ", ") + "\n")
	}
	if py {
		code := "import importlib.util as u;print(' '.join(m for m in " + pyList() + " if u.find_spec(m)))"
		mods := run("python3", "-c", code)
		missing := []string{}
		for _, m := range pyMods {
			if !strings.Contains(" "+mods+" ", " "+m+" ") {
				missing = append(missing, m)
			}
		}
		if mods != "" {
			b.WriteString("python3 has: " + mods + "\n")
		}
		if len(missing) > 0 {
			b.WriteString("python3 lacks: " + strings.Join(missing, " ") + "\n")
		}
	}
	b.WriteString("When a command fails because a program or module is missing, do not run it again unchanged: install it if the task needs it (python: a venv in the workspace, python3 -m venv .venv && .venv/bin/pip install <module>, since Homebrew python refuses pip installs; tools: brew install <tool>), or use what is installed, or tell the user what is missing.")
	return b.String()
}

func pyList() string {
	q := make([]string, len(pyMods))
	for i, m := range pyMods {
		q[i] = "'" + m + "'"
	}
	return "[" + strings.Join(q, ",") + "]"
}

// trimVersion keeps the version number out of a --version line.
func trimVersion(s string) string {
	for _, f := range strings.Fields(s) {
		f = strings.TrimPrefix(strings.TrimSuffix(f, ","), "go")
		f = strings.TrimPrefix(f, "v")
		if f != "" && f[0] >= '0' && f[0] <= '9' {
			return f
		}
	}
	return s
}
