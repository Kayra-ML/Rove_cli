package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aether-dev/aether/internal/permission"
	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/types"
)

func TestReadWritePatchAndJail(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New(nil)
	rt.Register(ReadFile{})
	rt.Register(WriteFile{})
	rt.Register(PatchFile{})
	rt.Register(ListDir{})
	tc := Context{Workspace: root}
	res, err := rt.Call(context.Background(), "read_file", tc, json.RawMessage(`{"path":"a.txt"}`))
	if err != nil || res.Content != "hello" {
		t.Fatalf("%v %v", res, err)
	}
	res, err = rt.Call(context.Background(), "write_file", tc, json.RawMessage(`{"path":"b.txt","content":"world"}`))
	if err != nil {
		t.Fatal(err)
	}
	res, err = rt.Call(context.Background(), "patch_file", tc, json.RawMessage(`{"path":"b.txt","old_string":"world","new_string":"WORLD"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(b) != "WORLD" {
		t.Fatalf("got %s", b)
	}
	_, err = rt.Call(context.Background(), "read_file", tc, json.RawMessage(`{"path":"../x"}`))
	if err == nil {
		t.Fatal("expected jail error")
	}
	_ = res
}

func TestListDir(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "x.go"), []byte("package x"), 0o644)
	os.Mkdir(filepath.Join(root, "sub"), 0o755)
	rt := New(nil)
	rt.Register(ListDir{})
	tc := Context{Workspace: root}
	res, err := rt.Call(context.Background(), "list_dir", tc, json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "x.go") || !strings.Contains(res.Content, "sub/") {
		t.Fatalf("got %q", res.Content)
	}
	_, err = rt.Call(context.Background(), "list_dir", tc, json.RawMessage(`{"path":".."}`))
	if err == nil {
		t.Fatal("expected escape error")
	}
}

func TestReadFileOffsetLimit(t *testing.T) {
	root := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5"
	os.WriteFile(filepath.Join(root, "f.txt"), []byte(content), 0o644)
	rt := New(nil)
	rt.Register(ReadFile{})
	tc := Context{Workspace: root}
	res, err := rt.Call(context.Background(), "read_file", tc, json.RawMessage(`{"path":"f.txt","offset":2,"limit":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "line2\nline3" {
		t.Fatalf("got %q", res.Content)
	}
}

func TestPatchFileNotFound(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.txt"), []byte("hello world"), 0o644)
	rt := New(nil)
	rt.Register(PatchFile{})
	tc := Context{Workspace: root}
	res, _ := rt.Call(context.Background(), "patch_file", tc, json.RawMessage(`{"path":"p.txt","old_string":"nope","new_string":"yes"}`))
	if !res.IsError {
		t.Fatal("expected error on missing old_string")
	}
}

func TestPatchFileNotUnique(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "dup.txt"), []byte("x x x"), 0o644)
	rt := New(nil)
	rt.Register(PatchFile{})
	tc := Context{Workspace: root}
	res, _ := rt.Call(context.Background(), "patch_file", tc, json.RawMessage(`{"path":"dup.txt","old_string":"x","new_string":"y"}`))
	if !res.IsError {
		t.Fatal("expected not-unique error")
	}
}

func TestGitStatusToolNoWorkspace(t *testing.T) {
	rt := New(nil)
	rt.Register(GitStatusTool{})
	// A temp dir that is NOT a git repo → git rev-parse fails → IsError=true
	notGit := t.TempDir()
	res, _ := rt.Call(context.Background(), "git_status", Context{Workspace: notGit}, json.RawMessage(`{}`))
	if !res.IsError {
		t.Fatal("expected IsError=true for non-git workspace")
	}
}

func TestToolSpecs(t *testing.T) {
	rt := New(nil)
	rt.Register(ReadFile{})
	rt.Register(WriteFile{})
	rt.Register(PatchFile{})
	rt.Register(ListDir{})
	rt.Register(Shell{})
	specs := rt.Specs()
	if len(specs) != 5 {
		t.Fatalf("expected 5 specs, got %d", len(specs))
	}
	for _, s := range specs {
		if s.Name == "" || len(s.Parameters) == 0 {
			t.Fatalf("empty spec: %+v", s)
		}
	}
}

func TestShellAndPermissionDeny(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	eng := permission.New(st)
	if err := eng.Put(context.Background(), types.PermissionRule{ID: "d", Action: types.PermShell, Pattern: "*", Decision: types.PermDeny}); err != nil {
		t.Fatal(err)
	}
	rt := New(eng)
	rt.Register(Shell{})
	_, err = rt.Call(context.Background(), "shell", Context{Workspace: root}, json.RawMessage(`{"command":"echo hi"}`))
	if err == nil {
		t.Fatal("expected deny")
	}
	rt2 := New(nil)
	rt2.Register(Shell{})
	res, err := rt2.Call(context.Background(), "shell", Context{Workspace: root}, json.RawMessage(`{"command":"echo hi"}`))
	if err != nil || !strings.Contains(res.Content, "hi") {
		t.Fatalf("%v %v", res, err)
	}
}