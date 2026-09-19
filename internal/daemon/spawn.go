package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
)

type spawnSpec struct {
	path string
	args []string
}

// Command locates the product daemon without ever treating a Wails/desktop
// binary as the daemon itself. Prefers `rovecode daemon`, then legacy `aetherd`.
func Command() *exec.Cmd {
	var specs []spawnSpec
	if self, err := os.Executable(); err == nil {
		dir := filepath.Dir(self)
		specs = append(specs,
			spawnSpec{filepath.Join(dir, "rovecode"), []string{"daemon"}},
			spawnSpec{filepath.Join(dir, "rovecode.exe"), []string{"daemon"}},
			spawnSpec{filepath.Join(dir, "aetherd"), nil},
			spawnSpec{filepath.Join(dir, "aetherd.exe"), nil},
		)
	}
	if p, err := exec.LookPath("rovecode"); err == nil {
		specs = append(specs, spawnSpec{p, []string{"daemon"}})
	}
	if p, err := exec.LookPath("aetherd"); err == nil {
		specs = append(specs, spawnSpec{p, nil})
	}
	seen := map[string]bool{}
	for _, s := range specs {
		if s.path == "" || seen[s.path] {
			continue
		}
		seen[s.path] = true
		if _, err := os.Stat(s.path); err != nil {
			continue
		}
		return exec.Command(s.path, s.args...)
	}
	return nil
}
