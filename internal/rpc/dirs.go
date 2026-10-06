package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// dirList is one folder of the daemon's machine, for picking a project
// folder there (the native picker only sees this computer's disk).
type dirList struct {
	Path   string   `json:"path"`
	Parent string   `json:"parent"`
	Home   string   `json:"home"`
	Dirs   []string `json:"dirs"`
	// Project is set when the folder looks like a project (git, go.mod…).
	Project bool `json:"project"`
}

const maxDirs = 500

func (s *Server) handleDirs(_ context.Context, req protocol.Request) (json.RawMessage, error) {
	var p struct {
		Path   string `json:"path"`
		Hidden bool   `json:"hidden"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
	}
	home, _ := os.UserHomeDir()
	dir := strings.TrimSpace(p.Path)
	if dir == "" {
		dir = home
	}
	if strings.HasPrefix(dir, "~") {
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	dir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := dirList{Path: dir, Parent: filepath.Dir(dir), Home: home, Dirs: []string{}}
	if out.Parent == dir {
		out.Parent = ""
	}
	for _, e := range entries {
		name := e.Name()
		switch name {
		case ".git", "go.mod", "package.json", "Cargo.toml", "pyproject.toml":
			out.Project = true
		}
		if !e.IsDir() || (!p.Hidden && strings.HasPrefix(name, ".")) || name == "node_modules" {
			continue
		}
		if len(out.Dirs) < maxDirs {
			out.Dirs = append(out.Dirs, name)
		}
	}
	sort.Slice(out.Dirs, func(i, j int) bool { return strings.ToLower(out.Dirs[i]) < strings.ToLower(out.Dirs[j]) })
	return core.MustJSON(out), nil
}
