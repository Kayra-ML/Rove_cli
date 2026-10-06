package checkpoint

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Snapshot represents a single git-stash snapshot.
type Snapshot struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Ref       string    `json:"ref"`
	CreatedAt time.Time `json:"createdAt"`
}

// Manager provides snapshot/restore operations via git stash.
type Manager struct {
	GitBin string
}

// New returns a Manager using the system git binary.
func New() *Manager {
	return &Manager{GitBin: "git"}
}

func (m *Manager) run(dir string, args ...string) (string, error) {
	return m.runEnv(dir, nil, args...)
}

func (m *Manager) runEnv(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command(m.GitBin, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ErrNothingToSnapshot means the working tree has no changes to save.
var ErrNothingToSnapshot = errors.New("nothing to snapshot: working tree is clean")

// Snapshot commits are internal; they must not depend on (or borrow) the
// user's git identity.
var snapshotIdentity = []string{
	"GIT_AUTHOR_NAME=Rove checkpoint", "GIT_AUTHOR_EMAIL=checkpoint@rove.local",
	"GIT_COMMITTER_NAME=Rove checkpoint", "GIT_COMMITTER_EMAIL=checkpoint@rove.local",
}

// Take saves the working tree — tracked changes, deletions and untracked
// (not ignored) files — as a snapshot, without touching the tree or the
// index at any moment. The agent takes one before every write or shell
// call, and team members may be writing in the same workspace at the same
// time, so a snapshot must never move a file, not even briefly (as
// `git stash push` would, by resetting the tree before it is re-applied).
//
// It builds the snapshot in a throwaway index and files the commit in the
// stash list, where List, Restore and Drop find it.
func (m *Manager) Take(path, label string) (Snapshot, error) {
	if label == "" {
		label = "checkpoint-" + time.Now().UTC().Format("20060102-150405")
	}
	head, err := m.run(path, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil || head == "" {
		return Snapshot{}, fmt.Errorf("no commit to snapshot against in %s", path)
	}
	idx, err := os.CreateTemp("", "rove-checkpoint-index-*")
	if err != nil {
		return Snapshot{}, err
	}
	idx.Close()
	defer os.Remove(idx.Name())
	env := append([]string{"GIT_INDEX_FILE=" + idx.Name()}, snapshotIdentity...)
	if _, err := m.runEnv(path, env, "read-tree", "HEAD"); err != nil {
		return Snapshot{}, err
	}
	if _, err := m.runEnv(path, env, "add", "--all", "--", "."); err != nil {
		return Snapshot{}, err
	}
	tree, err := m.runEnv(path, env, "write-tree")
	if err != nil {
		return Snapshot{}, err
	}
	if headTree, _ := m.run(path, "rev-parse", "HEAD^{tree}"); tree == headTree {
		return Snapshot{}, ErrNothingToSnapshot
	}
	// nothing changed since the last snapshot (the agent takes one before
	// every tool call): reuse it instead of piling up identical entries
	if last, err := m.run(path, "rev-parse", "--verify", "--quiet", "stash@{0}^{tree}"); err == nil && last == tree {
		return Snapshot{ID: "stash@{0}", Label: label, Ref: "stash@{0}", CreatedAt: time.Now().UTC()}, nil
	}
	// stash-shaped (parents: HEAD, an "index" commit) so the stash list takes
	// it; the staged/unstaged split is not kept, the index commit is HEAD's tree
	headTree, err := m.run(path, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return Snapshot{}, err
	}
	index, err := m.runEnv(path, env, "commit-tree", headTree, "-p", head, "-m", "index on "+label)
	if err != nil {
		return Snapshot{}, err
	}
	commit, err := m.runEnv(path, env, "commit-tree", tree, "-p", head, "-p", index, "-m", label)
	if err != nil {
		return Snapshot{}, err
	}
	if _, err := m.run(path, "stash", "store", "-m", label, commit); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Label:     label,
		Ref:       "stash@{0}",
		CreatedAt: time.Now().UTC(),
	}, nil
}

// List returns all snapshots in the stash for the given repo path.
func (m *Manager) List(path string) ([]Snapshot, error) {
	out, err := m.run(path, "stash", "list", "--format=%gd|%s|%ci")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return []Snapshot{}, nil
	}
	var snaps []Snapshot
	for i, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		ref := fmt.Sprintf("stash@{%d}", i)
		label := ""
		createdAt := time.Now().UTC()
		if len(parts) >= 1 {
			ref = parts[0]
		}
		if len(parts) >= 2 {
			label = strings.TrimPrefix(parts[1], "On ")
			// git stash list subject is "On <branch>: <message>" or "WIP on <branch>: <message>"
			if idx := strings.Index(label, ": "); idx != -1 {
				label = label[idx+2:]
			}
		}
		if len(parts) >= 3 {
			if t, err := time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(parts[2])); err == nil {
				createdAt = t
			}
		}
		snaps = append(snaps, Snapshot{
			ID:        ref,
			Label:     label,
			Ref:       ref,
			CreatedAt: createdAt,
		})
	}
	return snaps, nil
}

// Restore puts the working tree back to the snapshot at ref (e.g.
// "stash@{0}"), keeping the snapshot. What is there now is saved first as a
// "before-restore" snapshot, so a restore can itself be undone. Files come
// back as working-tree changes; the index is left matching HEAD.
func (m *Manager) Restore(path, ref string) error {
	// resolve first: saving the current state shifts every stash@{n}
	sha, err := m.run(path, "rev-parse", "--verify", "--quiet", ref)
	if err != nil || sha == "" {
		return fmt.Errorf("no snapshot %s", ref)
	}
	if _, err := m.Take(path, "before-restore"); err != nil && !errors.Is(err, ErrNothingToSnapshot) {
		return err
	}
	// clear what changed since (untracked files included; ignored ones stay)
	if _, err := m.run(path, "reset", "--hard", "--quiet"); err != nil {
		return err
	}
	if _, err := m.run(path, "clean", "-fd", "--quiet"); err != nil {
		return err
	}
	// lay the snapshot's tree down, deletions included, then unstage
	if _, err := m.run(path, "read-tree", "-u", "--reset", sha+"^{tree}"); err != nil {
		return err
	}
	// snapshots made by older versions (git stash push -u) keep untracked
	// files in a third parent
	if u, err := m.run(path, "rev-parse", "--verify", "--quiet", sha+"^3"); err == nil && u != "" {
		if _, err := m.run(path, "checkout", u, "--", "."); err != nil {
			return err
		}
	}
	_, err = m.run(path, "reset", "--quiet")
	return err
}

// Drop removes the snapshot identified by ref.
func (m *Manager) Drop(path, ref string) error {
	_, err := m.run(path, "stash", "drop", ref)
	return err
}
