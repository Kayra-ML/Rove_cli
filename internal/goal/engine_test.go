package goal

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/judge"
	"github.com/Kayra-ML/rove/internal/kanban"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

func TestDriveStopsOnPassingGates(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bus := eventbus.New()
	defer bus.Close()
	k := kanban.New(s, bus)
	r := provider.NewRouter()
	r.Register("fake", &provider.Fake{Responses: []string{"implemented hello"}})
	sess := session.New(s, bus)
	ag := agent.New(s, bus, sess, nil, r, tool.New(nil))
	ctx := context.Background()
	a, err := ag.Upsert(ctx, types.Agent{Name: "a", Provider: "fake", Model: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	j := judge.New(nil)
	e := New(s, bus, k, ag, j, nil, nil)
	g, err := e.Create(ctx, types.Goal{
		Title:       "hello",
		AgentID:     a.ID,
		Description: "say hello",
		CompletionContract: types.CompletionContract{
			Criteria:      []string{"hello"},
			QualityGates:  []types.QualityGate{{Name: "ok", Kind: types.GateTest, Command: "exit 0", ExpectExitZero: true}},
			MaxIterations: 3,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionObj, err := sess.Create(ctx, "g", a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	g, err = e.Drive(ctx, g.ID, sessionObj.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != types.GoalDone {
		t.Fatalf("status %s verdict %+v", g.Status, g.LastVerdict)
	}
	card, err := k.Get(ctx, g.CardID)
	if err != nil {
		t.Fatal(err)
	}
	if card.Column != types.ColReview {
		t.Fatalf("card column %s", card.Column)
	}
}

func TestDriveBlocksAtMaxIterations(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	j := judge.New(nil)
	e := New(s, nil, nil, nil, j, nil, nil)
	ctx := context.Background()
	g, err := e.Create(ctx, types.Goal{
		Title: "never",
		CompletionContract: types.CompletionContract{
			Criteria:      []string{"impossible-unique-token-xyz"},
			QualityGates:  []types.QualityGate{{Name: "fail", Kind: types.GateTest, Command: "exit 1", ExpectExitZero: true}},
			MaxIterations: 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err = e.Drive(ctx, g.ID, "", t.TempDir())
	if g.Status != types.GoalBlocked {
		t.Fatalf("expected blocked after max iterations, got %s err=%v verdict=%+v", g.Status, err, g.LastVerdict)
	}
}

// ── listRepoFiles ─────────────────────────────────────────────────────────────

func TestListRepoFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a minimal repo structure.
	mustCreate := func(rel string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustCreate("main.go")
	mustCreate("internal/foo/foo.go")
	mustCreate("internal/bar/bar.go")
	// These should be skipped.
	mustCreate("vendor/pkg/x.go")
	mustCreate(".git/HEAD")
	mustCreate("node_modules/lodash/index.js")

	files := listRepoFiles(dir)
	got := make(map[string]bool, len(files))
	for _, f := range files {
		got[f] = true
	}

	if !got["main.go"] {
		t.Error("missing main.go")
	}
	if !got["internal/foo/foo.go"] {
		t.Error("missing internal/foo/foo.go")
	}
	if got["vendor/pkg/x.go"] {
		t.Error("vendor files must be skipped")
	}
	if got[".git/HEAD"] {
		t.Error(".git files must be skipped")
	}
	if got["node_modules/lodash/index.js"] {
		t.Error("node_modules must be skipped")
	}
}

func TestListRepoFilesEmpty(t *testing.T) {
	files := listRepoFiles("")
	if len(files) != 0 {
		t.Errorf("expected empty slice for empty dir, got %v", files)
	}
}

func TestListRepoFilesMaxCap(t *testing.T) {
	dir := t.TempDir()
	// Create 10 files — well under cap, all should appear.
	for i := 0; i < 10; i++ {
		name := filepath.Join(dir, "f"+string(rune('0'+i))+".go")
		_ = os.WriteFile(name, []byte(""), 0o644)
	}
	files := listRepoFiles(dir)
	if len(files) != 10 {
		t.Errorf("expected 10 files, got %d", len(files))
	}
}
