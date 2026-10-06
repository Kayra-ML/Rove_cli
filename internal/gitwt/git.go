package gitwt

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Kayra-ML/rove/internal/types"
)

type Manager struct {
	GitBin string
}

func New() *Manager { return &Manager{GitBin: "git"} }

func (m *Manager) run(dir string, args ...string) (string, error) {
	cmd := exec.Command(m.GitBin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (m *Manager) IsRepo(path string) bool {
	_, err := m.run(path, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

func (m *Manager) Init(path string, branch string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	args := []string{"init"}
	if branch != "" {
		args = append(args, "-b", branch)
	}
	_, err := m.run(path, args...)
	return err
}

func (m *Manager) Status(path string) (types.GitStatus, error) {
	branch, err := m.run(path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return types.GitStatus{}, err
	}
	porcelain, err := m.run(path, "status", "--porcelain")
	if err != nil {
		return types.GitStatus{}, err
	}
	st := types.GitStatus{Branch: branch, Dirty: porcelain != ""}
	if porcelain != "" {
		for _, line := range strings.Split(porcelain, "\n") {
			if len(line) < 4 {
				continue
			}
			xy, file := line[:2], strings.TrimSpace(line[3:])
			if xy == "??" {
				st.Untracked = append(st.Untracked, file)
			} else {
				st.Changed = append(st.Changed, file)
			}
		}
	}
	return st, nil
}

func (m *Manager) AddAll(path string) error {
	_, err := m.run(path, "add", "-A")
	return err
}

func (m *Manager) Commit(path, message string) error {
	_, err := m.run(path, "commit", "-m", message, "--allow-empty")
	return err
}

func (m *Manager) CurrentBranch(path string) (string, error) {
	return m.run(path, "rev-parse", "--abbrev-ref", "HEAD")
}

func (m *Manager) CreateWorktree(repo, dest, branch string) (types.Worktree, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return types.Worktree{}, err
	}
	exists, _ := m.run(repo, "rev-parse", "--verify", branch)
	var err error
	if exists == "" {
		_, err = m.run(repo, "worktree", "add", "-b", branch, dest)
	} else {
		_, err = m.run(repo, "worktree", "add", dest, branch)
	}
	if err != nil {
		_, err = m.run(repo, "worktree", "add", "-B", branch, dest)
		if err != nil {
			return types.Worktree{}, err
		}
	}
	head, _ := m.run(dest, "rev-parse", "HEAD")
	return types.Worktree{Path: dest, Branch: branch, Head: head}, nil
}

func (m *Manager) RemoveWorktree(repo, dest string) error {
	_, err := m.run(repo, "worktree", "remove", "--force", dest)
	if err != nil {
		_ = os.RemoveAll(dest)
		_, err = m.run(repo, "worktree", "prune")
	}
	return err
}

func (m *Manager) ListWorktrees(repo string) ([]types.Worktree, error) {
	out, err := m.run(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var outw []types.Worktree
	var cur types.Worktree
	flush := func() {
		if cur.Path != "" {
			outw = append(outw, cur)
			cur = types.Worktree{}
		}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "":
			flush()
		}
	}
	flush()
	return outw, nil
}

func (m *Manager) CommitAll(path, message string) error {
	if err := m.AddAll(path); err != nil {
		return err
	}
	return m.Commit(path, message)
}

// GitCommit represents a single git log entry.
type GitCommit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

func (m *Manager) Diff(path string) (string, error) {
	staged, err := m.run(path, "diff", "--cached")
	if err != nil {
		return "", err
	}
	unstaged, err := m.run(path, "diff")
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(strings.TrimSpace(staged) + "\n" + unstaged)
	if out == "" {
		porcelain, perr := m.run(path, "status", "--porcelain")
		if perr != nil {
			return "", perr
		}
		return porcelain, nil
	}
	return out, nil
}

func (m *Manager) Log(path string, limit int) ([]GitCommit, error) {
	if limit <= 0 {
		limit = 20
	}
	format := "--pretty=format:%H\x1f%an\x1f%ai\x1f%s"
	out, err := m.run(path, "log", fmt.Sprintf("-n%d", limit), format)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return []GitCommit{}, nil
	}
	var commits []GitCommit
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\x1f", 4)
		if len(parts) != 4 {
			continue
		}
		commits = append(commits, GitCommit{
			Hash:    parts[0],
			Author:  parts[1],
			Date:    parts[2],
			Subject: parts[3],
		})
	}
	return commits, nil
}

// CreateBranch creates a new local branch at the given repo path.
func (m *Manager) CreateBranch(path, name string) error {
	_, err := m.run(path, "checkout", "-b", name)
	return err
}

// PushBranch pushes the given branch to the remote.
func (m *Manager) PushBranch(path, remote, branch string) error {
	_, err := m.run(path, "push", "-u", remote, branch)
	return err
}

// CreatePR creates a pull request using the gh CLI and returns the PR URL.
func (m *Manager) CreatePR(path, title, body, base string) (string, error) {
	// "--flag=value": a title starting with "-" stays a title
	args := []string{"pr", "create", "--title=" + title, "--body=" + body}
	if base != "" {
		args = append(args, "--base="+base)
	}
	cmd := exec.Command("gh", args...)
	cmd.Dir = path
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh pr create: %w (%s)", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ApplyHunk applies a single unified-diff hunk patch to the given directory.
// The patch string must be a valid unified diff (including file headers).
func (m *Manager) ApplyHunk(dir, hunkPatch string) error {
	cmd := exec.Command(m.GitBin, "apply", "--unidiff-zero", "-")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(hunkPatch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git apply: %w (%s)", err, stderr.String())
	}
	return nil
}

// RejectHunk reverses a single unified-diff hunk patch (i.e. undoes it).
func (m *Manager) RejectHunk(dir, hunkPatch string) error {
	cmd := exec.Command(m.GitBin, "apply", "--reverse", "--unidiff-zero", "-")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(hunkPatch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git apply --reverse: %w (%s)", err, stderr.String())
	}
	return nil
}

// ── Worktree isolation for subagents ──────────────────────────────────────────
//
// Parallel agents writing one checkout collide: two of them edit a file and
// the second quietly wins. Each subagent instead gets a checkout of its own,
// and what it changed comes back to the user's tree as a patch once it is
// done. The user's own tree, index and branch are never committed to: the
// changes land there uncommitted, exactly as a single agent's would.

// runIn is run with extra environment and, optionally, a stdin. raw keeps the
// output untouched — a patch must keep its final newline.
func (m *Manager) runIn(dir string, env []string, stdin string, raw bool, args ...string) (string, error) {
	cmd := exec.Command(m.GitBin, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	if raw {
		return stdout.String(), nil
	}
	return strings.TrimSpace(stdout.String()), nil
}

// snapshotIdentity signs the throwaway snapshot commit, so a machine with no
// git identity configured can still take one.
var snapshotIdentity = []string{
	"GIT_AUTHOR_NAME=Rove", "GIT_AUTHOR_EMAIL=rove@localhost",
	"GIT_COMMITTER_NAME=Rove", "GIT_COMMITTER_EMAIL=rove@localhost",
}

// Snapshot records the working tree as it stands — tracked changes and new
// files alike, ignored files not — as a commit on top of HEAD, without
// touching the user's index, tree or branch. A subagent's checkout starts
// from it, so it sees the work the user has not committed yet. It works on
// a copy of the index, which keeps git's cached file stats and so only
// re-reads files that changed.
func (m *Manager) Snapshot(repo string) (string, error) {
	head, err := m.run(repo, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("no commit to start from: %w", err)
	}
	real, err := m.run(repo, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "rove-index-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if b, rerr := os.ReadFile(real); rerr == nil {
		_, _ = tmp.Write(b)
	}
	_ = tmp.Close()
	env := []string{"GIT_INDEX_FILE=" + tmpPath}
	if _, err := m.runIn(repo, env, "", false, "add", "-A"); err != nil {
		return "", err
	}
	tree, err := m.runIn(repo, env, "", false, "write-tree")
	if err != nil {
		return "", err
	}
	if headTree, _ := m.run(repo, "rev-parse", head+"^{tree}"); headTree == tree {
		return head, nil // nothing uncommitted: HEAD is the snapshot
	}
	return m.runIn(repo, snapshotIdentity, "", false, "commit-tree", tree, "-p", head, "-m", "rove: working tree for subagents")
}

// AddWorktree checks commit out into dest on a new branch.
func (m *Manager) AddWorktree(repo, dest, branch, commit string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	_, err := m.run(repo, "worktree", "add", "-b", branch, dest, commit)
	return err
}

// WorkPatch is everything a checkout changed since base — edits, new files,
// deletions, binaries — as a patch, with the added and removed line counts
// and the files it touched. Commits the agent made there count too: the
// comparison is between base and what is in the checkout now.
func (m *Manager) WorkPatch(dest, base string) (patch string, added, removed int, files []string, err error) {
	if _, err = m.run(dest, "add", "-A"); err != nil {
		return "", 0, 0, nil, err
	}
	if patch, err = m.runIn(dest, nil, "", true, "diff", "--cached", "--binary", base); err != nil {
		return "", 0, 0, nil, err
	}
	stat, err := m.run(dest, "diff", "--cached", "--numstat", base)
	if err != nil {
		return "", 0, 0, nil, err
	}
	for _, line := range strings.Split(stat, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		// binary files show "-" for both counts
		var a, r int
		_, _ = fmt.Sscanf(parts[0], "%d", &a)
		_, _ = fmt.Sscanf(parts[1], "%d", &r)
		added += a
		removed += r
		files = append(files, parts[2])
	}
	return patch, added, removed, files, nil
}

// ApplyPatch lays a patch onto repo's working tree, leaving it uncommitted.
// It checks first, so a patch that does not fit — the same lines changed by
// someone else meanwhile — changes nothing at all rather than half of it.
func (m *Manager) ApplyPatch(repo, patch string) error {
	if strings.TrimSpace(patch) == "" {
		return nil
	}
	if _, err := m.runIn(repo, nil, patch, false, "apply", "--check", "--whitespace=nowarn", "-"); err != nil {
		return err
	}
	_, err := m.runIn(repo, nil, patch, false, "apply", "--whitespace=nowarn", "-")
	return err
}

// DropWorktree removes a subagent's checkout and its branch.
func (m *Manager) DropWorktree(repo, dest, branch string) error {
	err := m.RemoveWorktree(repo, dest)
	if branch != "" {
		if _, berr := m.run(repo, "branch", "-D", branch); berr != nil && err == nil {
			err = berr
		}
	}
	return err
}
