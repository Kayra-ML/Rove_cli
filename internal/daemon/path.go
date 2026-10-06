package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const pathMark = "__ROVE_PATH__"

// FixPath gives the daemon the PATH the person has in their own terminal.
// An app opened from the Finder or the Dock gets /usr/bin:/bin:/usr/sbin:/sbin,
// so the commands an agent runs found none of Homebrew, node, go, pyenv or
// what the person installed — and ran the system's python instead of
// theirs. The login shell is asked once; the usual install places are
// added after it in case it could not be asked. Returns where PATH came from.
func FixPath() string {
	if runtime.GOOS == "windows" {
		return "inherited"
	}
	src := "inherited"
	var parts []string
	if p := loginPath(); p != "" {
		parts = append(parts, filepath.SplitList(p)...)
		src = "login shell"
	}
	parts = append(parts, filepath.SplitList(os.Getenv("PATH"))...)
	if home, err := os.UserHomeDir(); err == nil {
		parts = append(parts, filepath.Join(home, ".local", "bin"), filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".cargo", "bin"), filepath.Join(home, ".bun", "bin"))
	}
	parts = append(parts, "/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	_ = os.Setenv("PATH", joinPath(parts))
	return src
}

// joinPath drops empty, relative and repeated entries, keeping the first.
func joinPath(parts []string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || !filepath.IsAbs(p) || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

// loginPath asks the person's login shell (interactive, so ~/.zshrc runs as
// in a terminal) for PATH. What the rc files print around it is cut off by
// the marks; a shell that hangs is given up on.
func loginPath() string {
	sh := os.Getenv("SHELL")
	if sh == "" || !filepath.IsAbs(sh) {
		sh = "/bin/zsh"
		if runtime.GOOS != "darwin" {
			sh = "/bin/sh"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sh, "-l", "-i", "-c", `printf '`+pathMark+`%s`+pathMark+`' "$PATH"`)
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "TERM=dumb")
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return ""
	}
	return between(string(out), pathMark)
}

func between(s, mark string) string {
	i := strings.Index(s, mark)
	if i < 0 {
		return ""
	}
	s = s[i+len(mark):]
	j := strings.Index(s, mark)
	if j < 0 {
		return ""
	}
	return s[:j]
}
