package gitwt

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aether-dev/aether/internal/types"
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

func (m *Manager) BranchForCard(cardID types.ID) string {
	return "aether/" + string(cardID)
}

func (m *Manager) WorktreePath(repo string, cardID types.ID) string {
	return filepath.Join(repo, ".aether", "worktrees", string(cardID))
}
