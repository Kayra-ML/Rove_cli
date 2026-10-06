package goal

import (
	"os/exec"
	"strings"
)

// Evidence for the reviewer: what a goal changed in its workspace.

// baseline is the workspace as the goal found it: a commit of the working
// tree (tracked changes included) and the untracked files already there.
type baseline struct {
	rev       string
	untracked map[string]bool
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}

// gitBase records the baseline; nil outside a git repository.
func gitBase(dir string) *baseline {
	if dir == "" {
		return nil
	}
	if _, err := git(dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil
	}
	// "stash create" snapshots tracked changes without touching anything;
	// a clean tree gives nothing, and HEAD is the baseline
	rev, _ := git(dir, "stash", "create")
	rev = strings.TrimSpace(rev)
	if rev == "" {
		head, err := git(dir, "rev-parse", "HEAD")
		if err != nil {
			return nil // no commits yet
		}
		rev = strings.TrimSpace(head)
	}
	b := &baseline{rev: rev, untracked: map[string]bool{}}
	for _, f := range untracked(dir) {
		b.untracked[f] = true
	}
	return b
}

func untracked(dir string) []string {
	out, err := git(dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil
	}
	return strings.Fields(out)
}

// workspaceChanges returns the diff stat and diff since the baseline, and
// the changed paths (new untracked files included).
func workspaceChanges(dir string, b *baseline) (stat, diff string, files []string) {
	if b == nil {
		return "", "", nil
	}
	stat, _ = git(dir, "diff", "--stat", b.rev)
	diff, _ = git(dir, "diff", b.rev)
	if names, err := git(dir, "diff", "--name-only", b.rev); err == nil {
		files = strings.Fields(names)
	}
	var fresh []string
	for _, f := range untracked(dir) {
		if !b.untracked[f] {
			fresh = append(fresh, f)
		}
	}
	if len(fresh) > 0 {
		files = append(files, fresh...)
		stat = strings.TrimRight(stat, "\n")
		if stat != "" {
			stat += "\n"
		}
		stat += "new files: " + strings.Join(fresh, ", ")
	}
	return strings.TrimSpace(stat), diff, files
}
