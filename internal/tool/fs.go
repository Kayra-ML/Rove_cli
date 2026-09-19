package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

func schema(props string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":` + props + `,"additionalProperties":false}`)
}

type ReadFile struct{}

func (ReadFile) Name() string        { return "read_file" }
func (ReadFile) Description() string { return "Read a UTF-8 text file relative to the workspace." }
func (ReadFile) Parameters() json.RawMessage {
	return schema(`{"path":{"type":"string"},"offset":{"type":"integer"},"limit":{"type":"integer"}}`)
}
func (ReadFile) RequiredPermission() types.PermissionAction { return types.PermFilesystem }
func (ReadFile) Call(_ context.Context, tc Context, args json.RawMessage) (Result, error) {
	var in struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	p, err := resolve(tc.Workspace, in.Path)
	if err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	text := string(b)
	if in.Offset > 0 || in.Limit > 0 {
		lines := strings.Split(text, "\n")
		start := in.Offset
		if start < 1 {
			start = 1
		}
		end := len(lines)
		if in.Limit > 0 && start-1+in.Limit < end {
			end = start - 1 + in.Limit
		}
		if start-1 >= len(lines) {
			text = ""
		} else {
			text = strings.Join(lines[start-1:end], "\n")
		}
	}
	return Result{Content: text}, nil
}

type WriteFile struct{}

func (WriteFile) Name() string        { return "write_file" }
func (WriteFile) Description() string { return "Write a UTF-8 text file, creating parent directories." }
func (WriteFile) Parameters() json.RawMessage {
	return schema(`{"path":{"type":"string"},"content":{"type":"string"}}`)
}
func (WriteFile) RequiredPermission() types.PermissionAction { return types.PermFilesystem }
func (WriteFile) Call(_ context.Context, tc Context, args json.RawMessage) (Result, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	p, err := resolve(tc.Workspace, in.Path)
	if err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	if err := os.WriteFile(p, []byte(in.Content), 0o644); err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	return Result{Content: "wrote " + in.Path}, nil
}

type PatchFile struct{}

func (PatchFile) Name() string        { return "patch_file" }
func (PatchFile) Description() string { return "Replace old_string with new_string in a file." }
func (PatchFile) Parameters() json.RawMessage {
	return schema(`{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}}`)
}
func (PatchFile) RequiredPermission() types.PermissionAction { return types.PermFilesystem }
func (PatchFile) Call(_ context.Context, tc Context, args json.RawMessage) (Result, error) {
	var in struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	p, err := resolve(tc.Workspace, in.Path)
	if err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	n := bytes.Count(b, []byte(in.OldString))
	if n == 0 {
		return Result{IsError: true, Content: "old_string not found"}, fmt.Errorf("old_string not found")
	}
	if n > 1 {
		return Result{IsError: true, Content: "old_string not unique"}, fmt.Errorf("old_string not unique")
	}
	out := bytes.Replace(b, []byte(in.OldString), []byte(in.NewString), 1)
	if err := os.WriteFile(p, out, 0o644); err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	return Result{Content: "patched " + in.Path}, nil
}

type ListDir struct{}

func (ListDir) Name() string        { return "list_dir" }
func (ListDir) Description() string { return "List files in a directory relative to the workspace." }
func (ListDir) Parameters() json.RawMessage {
	return schema(`{"path":{"type":"string"}}`)
}
func (ListDir) RequiredPermission() types.PermissionAction { return types.PermFilesystem }
func (ListDir) Call(_ context.Context, tc Context, args json.RawMessage) (Result, error) {
	var in struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(args, &in)
	p, err := resolve(tc.Workspace, in.Path)
	if err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	var b strings.Builder
	for _, e := range ents {
		if e.IsDir() {
			b.WriteString(e.Name())
			b.WriteString("/\n")
		} else {
			b.WriteString(e.Name())
			b.WriteByte('\n')
		}
	}
	return Result{Content: b.String()}, nil
}

type Shell struct {
	Timeout time.Duration
}

func (Shell) Name() string        { return "shell" }
func (Shell) Description() string { return "Run a shell command in the workspace. Prefer non-interactive commands." }
func (Shell) Parameters() json.RawMessage {
	return schema(`{"command":{"type":"string"},"cwd":{"type":"string"}}`)
}
func (Shell) RequiredPermission() types.PermissionAction { return types.PermShell }
func (s Shell) Call(ctx context.Context, tc Context, args json.RawMessage) (Result, error) {
	var in struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return Result{IsError: true, Content: err.Error()}, err
	}
	cwd := tc.Workspace
	if in.Cwd != "" {
		p, err := resolve(tc.Workspace, in.Cwd)
		if err != nil {
			return Result{IsError: true, Content: err.Error()}, err
		}
		cwd = p
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shellBin(), shellArg(), in.Command)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	res := Result{Content: string(out)}
	if err != nil {
		res.IsError = true
		res.Content += "\n" + err.Error()
	}
	return res, nil
}

func resolve(root, rel string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("no workspace")
	}
	if rel == "" {
		rel = "."
	}
	clean := filepath.Clean(rel)
	abs := clean
	if !filepath.IsAbs(clean) {
		abs = filepath.Join(root, clean)
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	relOut, err := filepath.Rel(rootAbs, abs)
	if err != nil || strings.HasPrefix(relOut, "..") {
		return "", fmt.Errorf("path escapes workspace: %s", rel)
	}
	return abs, nil
}
