package codemap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kayra-ML/rove/internal/tool"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestParseGo(t *testing.T) {
	src := `package agent

import (
	"context"
	st "example.com/app/internal/store"
	"example.com/app/internal/types"
)

type Runtime struct{}

type (
	RunRequest struct{}
	runState int
)

func (rt *Runtime) Run(ctx context.Context) error {
	helper()
	_ = types.ID("x")
	return st.Open()
}

func helper() {}
`
	p := ParseFile("internal/agent/runtime.go", src)
	if p.Lang != "go" || p.GoPkg != "agent" {
		t.Fatalf("lang/pkg = %s/%s", p.Lang, p.GoPkg)
	}
	names := map[string]bool{}
	for _, s := range p.Symbols {
		names[s.Name] = true
	}
	for _, want := range []string{"Runtime", "RunRequest", "runState", "Run", "helper"} {
		if !names[want] {
			t.Errorf("missing symbol %s in %+v", want, p.Symbols)
		}
	}
	if p.GoAliases["st"] != "example.com/app/internal/store" || p.GoAliases["types"] != "example.com/app/internal/types" {
		t.Errorf("aliases = %v", p.GoAliases)
	}
	refs := strings.Join(p.GoRefs, ",")
	if !strings.Contains(refs, "st.Open") || !strings.Contains(refs, "types.ID") {
		t.Errorf("refs = %s", refs)
	}
	var sawHelper bool
	for _, c := range p.Calls {
		if c.Caller == "Run" && c.Name == "helper" {
			sawHelper = true
		}
	}
	if !sawHelper {
		t.Errorf("calls = %+v", p.Calls)
	}
}

func TestParseTSAndPython(t *testing.T) {
	ts := `import { rpc } from "~/lib/rpc";
import type { Session } from "../lib/types";
import "./side.css";
export function Chat() { return rpc("x"); }
export const useThing = (a: number): number => a;
class Local {}
`
	p := ParseFile("src/app/Chat.tsx", ts)
	if got := strings.Join(p.Imports, ","); got != "~/lib/rpc,../lib/types,./side.css" {
		t.Errorf("imports = %s", got)
	}
	if got := strings.Join(p.Exports, ","); !strings.Contains(got, "Chat") || !strings.Contains(got, "useThing") {
		t.Errorf("exports = %s", got)
	}

	py := `from .graph import query
from . import parser, schemas
import os, json
class Index:
    def load(self):
        return query()
def _private():
    pass
def rebuild():
    Index().load()
`
	q := ParseFile("pkg/tools.py", py)
	imps := strings.Join(q.Imports, ",")
	for _, want := range []string{".graph", ".parser", ".schemas", "os", "json"} {
		if !strings.Contains(imps, want) {
			t.Errorf("py imports %s missing %s", imps, want)
		}
	}
	if strings.Contains(strings.Join(q.Exports, ","), "_private") {
		t.Errorf("private leaked into exports: %v", q.Exports)
	}
}

func TestIndexResolvesAcrossLanguages(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod":                    "module example.com/app\n\ngo 1.22\n",
		"internal/store/store.go":   "package store\n\nfunc Open() error { return nil }\n",
		"internal/store/other.go":   "package store\n\nfunc Close() {}\n",
		"internal/types/types.go":   "package types\n\ntype ID string\n",
		"internal/agent/runtime.go": "package agent\n\nimport (\n\t\"example.com/app/internal/store\"\n\t\"example.com/app/internal/types\"\n)\n\nfunc Run() { _ = types.ID(\"\"); store.Open(); local() }\n",
		"internal/agent/local.go":   "package agent\n\nfunc local() {}\n",
		"web/src/lib/rpc.ts":        "export function rpc() {}\n",
		"web/src/lib/index.ts":      "export * from './rpc';\n",
		"web/src/app/App.tsx":       "import { rpc } from '~/lib/rpc';\nimport { x } from '../lib';\nexport function App() { rpc(); }\n",
		"py/pkg/__init__.py":        "",
		"py/pkg/graph.py":           "def query():\n    pass\n",
		"py/pkg/tools.py":           "from .graph import query\n\ndef run():\n    query()\n",
		"node_modules/dep/index.js": "export function nope() {}\n",
		".hidden/secret.go":         "package hidden\n",
	})
	svc := NewService(t.TempDir())
	st, err := svc.Rebuild(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != 11 {
		t.Fatalf("files = %d, want 11 (node_modules and dotdirs skipped)", st.Files)
	}
	err = svc.With(root, func(ix *Index) error {
		want := map[[2]string]string{
			{"internal/agent/runtime.go", "internal/store/store.go"}: "import",
			{"internal/agent/runtime.go", "internal/types/types.go"}: "import",
			{"internal/agent/runtime.go", "internal/agent/local.go"}: "call",
			{"web/src/app/App.tsx", "web/src/lib/rpc.ts"}:            "import",
			{"web/src/app/App.tsx", "web/src/lib/index.ts"}:          "import",
			{"py/pkg/tools.py", "py/pkg/graph.py"}:                   "import",
		}
		got := map[[2]string]string{}
		for _, e := range ix.edges {
			got[[2]string{e.From, e.To}] = e.Kind
		}
		for k, kind := range want {
			if got[k] != kind {
				t.Errorf("edge %v = %q, want %q", k, got[k], kind)
			}
		}
		if _, bad := got[[2]string{"internal/agent/runtime.go", "internal/store/other.go"}]; bad {
			t.Error("runtime.go should only link to the store file that defines Open")
		}

		n, ok := ix.Neighbors("store.go", "both", 10)
		if !ok || len(n.ImportedBy) != 1 || n.ImportedBy[0] != "internal/agent/runtime.go" {
			t.Errorf("neighbors = %+v", n)
		}
		path, ok := ix.Path("internal/types/types.go", "internal/store/store.go")
		if !ok || len(path) != 3 {
			t.Errorf("path = %v", path)
		}
		if imp := ix.Impact([]string{"internal/agent/local.go"}, 2); len(imp) != 1 || imp[0] != "internal/agent/runtime.go" {
			t.Errorf("impact = %v", imp)
		}
		q := ix.Query("rpc", "all", 5, nil)
		if len(q.Hits) == 0 || q.Hits[0].File != "web/src/lib/rpc.ts" {
			t.Errorf("query hits = %+v", q.Hits)
		}
		g := ix.Graph(3, "")
		if len(g.Nodes) != 3 || !g.Truncated {
			t.Errorf("graph limit: %d nodes truncated=%v", len(g.Nodes), g.Truncated)
		}
		if brief := ix.Brief([]string{"runtime.go"}, 4); !strings.Contains(brief, "; uses: ") {
			t.Errorf("brief = %q", brief)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTouchAndPersist(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.py": "def alpha():\n    pass\n",
	})
	dir := t.TempDir()
	svc := NewService(dir)
	if _, err := svc.Rebuild(root, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.py"), []byte("from a import alpha\n\ndef beta():\n    alpha()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := svc.Touch(root, []string{filepath.Join(root, "b.py")})
	if err != nil || st.Added != 1 {
		t.Fatalf("touch = %+v, %v", st, err)
	}
	svc.Flush()
	_, _ = svc.Rebuild(root, false) // flushes regardless

	// A fresh service reads the persisted index back.
	svc2 := NewService(dir)
	_ = svc2.With(root, func(ix *Index) error {
		if len(ix.Files) != 2 {
			t.Errorf("reloaded files = %d", len(ix.Files))
		}
		if n, ok := ix.Neighbors("b.py", "out", 5); !ok || len(n.Imports) != 1 || n.Imports[0] != "a.py" {
			t.Errorf("reloaded neighbors = %+v", n)
		}
		return nil
	})

	// Deleting a file via Touch removes it.
	_ = os.Remove(filepath.Join(root, "b.py"))
	st, _ = svc2.Touch(root, []string{"b.py"})
	if st.Removed != 1 {
		t.Errorf("remove via touch = %+v", st)
	}
}

func TestRefuseHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home")
	}
	if _, err := NewService(t.TempDir()).Rebuild(home, false); err == nil {
		t.Fatal("indexing $HOME must be refused")
	}
	if _, err := NewService(t.TempDir()).Rebuild("", false); err == nil {
		t.Fatal("empty root must be refused")
	}
}

func TestToolsRunAgainstWorkspace(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/a.ts": "import { b } from './b';\nexport function a() { b(); }\n",
		"src/b.ts": "export function b() {}\n",
	})
	svc := NewService(t.TempDir())
	byName := map[string]tool.Tool{}
	for _, tl := range Tools(svc) {
		byName[tl.Name()] = tl
	}
	res, err := byName["graph_neighbors"].Call(context.Background(), tool.Context{Workspace: root}, json.RawMessage(`{"target":"b.ts"}`))
	if err != nil {
		t.Fatal(err)
	}
	var n NeighborsResult
	if err := json.Unmarshal([]byte(res.Content), &n); err != nil {
		t.Fatal(err)
	}
	if len(n.ImportedBy) != 1 || n.ImportedBy[0] != "src/a.ts" {
		t.Errorf("tool neighbors = %s", res.Content)
	}
	res, _ = byName["graph_impact"].Call(context.Background(), tool.Context{Workspace: root}, json.RawMessage(`{"files":["src/b.ts"]}`))
	if !strings.Contains(res.Content, "src/a.ts") {
		t.Errorf("tool impact = %s", res.Content)
	}
	for _, tl := range Tools(svc) {
		var schema map[string]any
		if err := json.Unmarshal(tl.Parameters(), &schema); err != nil {
			t.Errorf("%s schema: %v", tl.Name(), err)
		}
	}
}

func TestGoCallersStayInPackage(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod":     "module example.com/app\n",
		"a/new.go":   "package a\n\nfunc New() {}\n",
		"a/use.go":   "package a\n\nfunc Use() { New() }\n",
		"b/other.go": "package b\n\nfunc New() {}\n\nfunc Build() { New() }\n",
		"web/grp.ts": "export function groups() { New(); }\n",
	})
	svc := NewService(t.TempDir())
	_ = svc.With(root, func(ix *Index) error {
		n, _ := ix.Neighbors("a/new.go", "in", 20)
		if len(n.CalledBy) != 1 || n.CalledBy[0].File != "a/use.go" {
			t.Errorf("calledBy = %+v", n.CalledBy)
		}
		return nil
	})
}

// A Go file's constants and variables are symbols too, and a sibling that
// only names them — or a type, or a method's receiver — still depends on it.
func TestGoValuesAndSamePackageUses(t *testing.T) {
	src := "package team\n\nconst DelegateName = \"team_delegate\"\n\nvar ErrBusy = errors.New(\"busy\")\n\nconst (\n\tmaxParallel = 4 // cap\n\tA, B int = 1, 2\n\t_ = 0\n)\n\nvar (\n\tdefaults = Config{\n\t\tInner: 1,\n\t}\n)\n\ntype Config struct{ Inner int }\n"
	p := ParseFile("internal/team/delegate.go", src)
	kinds := map[string]string{}
	for _, s := range p.Symbols {
		kinds[s.Name] = s.Kind
	}
	for name, kind := range map[string]string{"DelegateName": "const", "ErrBusy": "var", "maxParallel": "const", "A": "const", "B": "const", "defaults": "var", "Config": "type"} {
		if kinds[name] != kind {
			t.Errorf("%s kind = %q, want %q (symbols %v)", name, kinds[name], kind, kinds)
		}
	}
	for _, bad := range []string{"_", "Inner"} {
		if _, ok := kinds[bad]; ok {
			t.Errorf("%q should not be a symbol", bad)
		}
	}
	if p.LOC != strings.Count(src, "\n") {
		t.Errorf("loc = %d, want %d", p.LOC, strings.Count(src, "\n"))
	}

	root := writeTree(t, map[string]string{
		"go.mod":                       "module example.com/app\n\ngo 1.22\n",
		"internal/team/delegate.go":    src,
		"internal/team/roster.go":      "package team\n\n// uses Config in a comment only: \"maxParallel\"\ntype Roster struct{ cfg *Config }\n\nfunc (r *Roster) Cap() int { return maxParallel }\n",
		"internal/team/roster_test.go": "package team\n\nimport \"testing\"\n\nfunc TestRoster(t *testing.T) { _ = Roster{} }\n",
		"internal/rpc/team.go":         "package rpc\n\nimport \"example.com/app/internal/team\"\n\nvar name = team.DelegateName\n",
	})
	svc := NewService(t.TempDir())
	if _, err := svc.Rebuild(root, true); err != nil {
		t.Fatal(err)
	}
	_ = svc.With(root, func(ix *Index) error {
		got := map[[2]string]bool{}
		for _, e := range ix.edges {
			got[[2]string{e.From, e.To}] = true
		}
		for _, k := range [][2]string{
			{"internal/team/roster.go", "internal/team/delegate.go"},
			{"internal/team/roster_test.go", "internal/team/roster.go"},
			{"internal/rpc/team.go", "internal/team/delegate.go"},
		} {
			if !got[k] {
				t.Errorf("missing edge %v", k)
			}
		}
		if q := ix.Query("DelegateName", "all", 5, nil); len(q.Hits) == 0 || q.Hits[0].File != "internal/team/delegate.go" {
			t.Errorf("query DelegateName = %+v", q.Hits)
		}
		return nil
	})
}
