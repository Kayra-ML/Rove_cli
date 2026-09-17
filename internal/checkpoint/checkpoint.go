package checkpoint

import (
	"bytes"
	"fmt"
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
	cmd := exec.Command(m.GitBin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Take creates a new snapshot (git stash) with the given label and returns its metadata.
func (m *Manager) Take(path, label string) (Snapshot, error) {
	if label == "" {
		label = "checkpoint-" + time.Now().UTC().Format("20060102-150405")
	}
	out, err := m.run(path, "stash", "push", "--include-untracked", "-m", label)
	if err != nil {
		return Snapshot{}, err
	}
	if strings.Contains(out, "No local changes") {
		return Snapshot{}, fmt.Errorf("nothing to snapshot: working tree is clean")
	}
	// After push the new stash is stash@{0}
	snap := Snapshot{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Label:     label,
		Ref:       "stash@{0}",
		CreatedAt: time.Now().UTC(),
	}
	return snap, nil
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

// Restore applies the snapshot identified by ref (e.g. "stash@{0}") without dropping it.
func (m *Manager) Restore(path, ref string) error {
	_, err := m.run(path, "stash", "apply", ref)
	return err
}

// Drop removes the snapshot identified by ref.
func (m *Manager) Drop(path, ref string) error {
	_, err := m.run(path, "stash", "drop", ref)
	return err
}