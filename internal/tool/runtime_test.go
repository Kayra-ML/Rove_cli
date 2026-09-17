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
