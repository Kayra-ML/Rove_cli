package ctxmap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

type fakeRunner struct {
	mu    sync.Mutex
	calls []agent.RunRequest
	busy  map[types.ID]int // remaining Busy()==true answers
	edits []string         // FilesEdited returned by each run
}

func (f *fakeRunner) Run(_ context.Context, req agent.RunRequest) (agent.RunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	return agent.RunResult{Assistant: "etkilenmedi", FilesEdited: f.edits, Done: true}, nil
}

func (f *fakeRunner) Busy(id types.ID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.busy[id] > 0 {
		f.busy[id]--
		return true
	}
	return false
}

func (f *fakeRunner) Calls() []agent.RunRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agent.RunRequest(nil), f.calls...)
}

type fixture struct {
	st     *store.Store
	eng    *Engine
	run    *fakeRunner
	a, b   types.Session
	wa, wb string
}

func setup(t *testing.T, targetFiles map[string]string) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	f := &fixture{st: st, run: &fakeRunner{busy: map[types.ID]int{}}}
	f.wa, f.wb = t.TempDir(), t.TempDir()
	for rel, body := range targetFiles {
		p := filepath.Join(f.wb, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o644)
	}
	now := time.Now().UTC()
	_ = st.UpsertWorkspace(ctx, types.Workspace{ID: "wa", Name: "web", Path: f.wa, CreatedAt: now, UpdatedAt: now})
	_ = st.UpsertWorkspace(ctx, types.Workspace{ID: "wb", Name: "desk", Path: f.wb, CreatedAt: now, UpdatedAt: now})
	f.a = types.Session{ID: "sa", Title: "Web sitesi", AgentID: "ag", WorkspaceID: "wa", CreatedAt: now, UpdatedAt: now}
	f.b = types.Session{ID: "sb", Title: "Desktop", AgentID: "ag", WorkspaceID: "wb", CreatedAt: now, UpdatedAt: now}
	_ = st.UpsertSession(ctx, f.a)
	_ = st.UpsertSession(ctx, f.b)
	f.eng = New(st, nil, f.run, DefaultFinder{})
	f.eng.Debounce = 30 * time.Millisecond
	f.eng.poll = 5 * time.Millisecond
	return f
}

func (f *fixture) link(t *testing.T, l types.SessionLink) types.SessionLink {
	t.Helper()
	if l.SessionA == "" {
		l.SessionA, l.SessionB = f.a.ID, f.b.ID
	}
	out, err := f.st.CreateSessionLink(context.Background(), l)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *fixture) relays(t *testing.T) []types.Relay {
	t.Helper()
	out, err := f.st.ListRelays(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestChangeReachesCounterpartWithTightBudget(t *testing.T) {
	f := setup(t, map[string]string{"assets/logo.png": "png", "src/Header.tsx": "export {}"})
	f.link(t, types.SessionLink{Auto: true})
	f.eng.OnRunDone(agent.RunRequest{SessionID: f.a.ID, Workspace: f.wa}, agent.RunResult{FilesEdited: []string{"public/logo.svg"}, Assistant: "Logoyu maviye çevirdim.\nDetay..."})
	f.eng.Wait()

	calls := f.run.Calls()
	if len(calls) != 1 {
		t.Fatalf("runs = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.SessionID != f.b.ID || c.Hop != 1 || c.MaxTurns != 6 || c.HistoryLimit != 10 {
		t.Fatalf("run request = %+v", c)
	}
	if len(c.Chain) != 1 || c.Chain[0] != f.a.ID {
		t.Fatalf("chain = %v", c.Chain)
	}
	for _, want := range []string{"public/logo.svg", "assets/logo.png", "Logoyu maviye çevirdim."} {
		if !strings.Contains(c.UserMessage, want) {
			t.Errorf("notice missing %q:\n%s", want, c.UserMessage)
		}
	}
	if strings.Contains(c.UserMessage, "Detay") {
		t.Error("only the first line of the source summary should travel")
	}
	rs := f.relays(t)
	if len(rs) != 1 || rs[0].Status != StatusDone || rs[0].Tokens == 0 || len(rs[0].Matches) == 0 {
		t.Fatalf("relay = %+v", rs)
	}
}

func TestNoCounterpartSkipsWithoutModel(t *testing.T) {
	f := setup(t, map[string]string{"src/main.go": "package main"})
	f.link(t, types.SessionLink{Auto: true})
	f.eng.OnRunDone(agent.RunRequest{SessionID: f.a.ID, Workspace: f.wa}, agent.RunResult{FilesEdited: []string{"public/logo.svg"}})
	f.eng.Wait()
	if n := len(f.run.Calls()); n != 0 {
		t.Fatalf("model ran %d times for an unrelated workspace", n)
	}
	rs := f.relays(t)
	if len(rs) != 1 || rs[0].Status != StatusSkipped || rs[0].Tokens != 0 {
		t.Fatalf("relay = %+v", rs)
	}
}

func TestAlwaysModeSkipsPrecheck(t *testing.T) {
	f := setup(t, nil)
	f.link(t, types.SessionLink{Auto: true, Mode: types.LinkAlways})
	f.eng.OnRunDone(agent.RunRequest{SessionID: f.a.ID, Workspace: f.wa}, agent.RunResult{FilesEdited: []string{"x.css"}})
	f.eng.Wait()
	if n := len(f.run.Calls()); n != 1 {
		t.Fatalf("always mode runs = %d", n)
	}
}

func TestBurstsCoalesceIntoOneRun(t *testing.T) {
	f := setup(t, map[string]string{"logo.svg": "", "theme.css": ""})
	f.link(t, types.SessionLink{Auto: true})
	req := agent.RunRequest{SessionID: f.a.ID, Workspace: f.wa}
	f.eng.OnRunDone(req, agent.RunResult{FilesEdited: []string{"a/logo.svg"}})
	f.eng.OnRunDone(req, agent.RunResult{FilesEdited: []string{"a/theme.css"}})
	f.eng.Wait()
	calls := f.run.Calls()
	if len(calls) != 1 {
		t.Fatalf("runs = %d, want one coalesced run", len(calls))
	}
	if !strings.Contains(calls[0].UserMessage, "a/logo.svg") || !strings.Contains(calls[0].UserMessage, "a/theme.css") {
		t.Fatalf("coalesced notice:\n%s", calls[0].UserMessage)
	}
}

func TestHopLimitDirectionAndManualOnly(t *testing.T) {
	f := setup(t, map[string]string{"logo.svg": ""})
	f.link(t, types.SessionLink{Auto: true, Direction: types.LinkBToA})
	// a2b blocked by direction
	f.eng.OnRunDone(agent.RunRequest{SessionID: f.a.ID}, agent.RunResult{FilesEdited: []string{"logo.svg"}})
	// relay-triggered run (hop 1) never forwards with the default MaxHops
	f.eng.OnRunDone(agent.RunRequest{SessionID: f.b.ID, Hop: 1}, agent.RunResult{FilesEdited: []string{"logo.svg"}})
	f.eng.Wait()
	if n := len(f.run.Calls()); n != 0 {
		t.Fatalf("runs = %d, want 0", n)
	}

	g := setup(t, map[string]string{"logo.svg": ""})
	g.link(t, types.SessionLink{Auto: false})
	g.eng.OnRunDone(agent.RunRequest{SessionID: g.a.ID}, agent.RunResult{FilesEdited: []string{"logo.svg"}})
	g.eng.Wait()
	if n := len(g.run.Calls()); n != 0 {
		t.Fatalf("manual-only cable ran %d times", n)
	}
}

func TestChainStopsPingPong(t *testing.T) {
	f := setup(t, map[string]string{"logo.svg": ""})
	f.eng.MaxHops = 5
	f.link(t, types.SessionLink{Auto: true})
	// B's run was itself triggered from A: A is in the chain, so nothing
	// goes back to A.
	f.eng.OnRunDone(agent.RunRequest{SessionID: f.b.ID, Hop: 1, Chain: []types.ID{f.a.ID}}, agent.RunResult{FilesEdited: []string{"logo.svg"}})
	f.eng.Wait()
	if n := len(f.run.Calls()); n != 0 {
		t.Fatalf("ping-pong runs = %d", n)
	}
}

func TestBusyTargetWaits(t *testing.T) {
	f := setup(t, map[string]string{"logo.svg": ""})
	f.link(t, types.SessionLink{Auto: true})
	f.run.busy[f.b.ID] = 3
	f.eng.OnRunDone(agent.RunRequest{SessionID: f.a.ID}, agent.RunResult{FilesEdited: []string{"logo.svg"}})
	f.eng.Wait()
	if n := len(f.run.Calls()); n != 1 {
		t.Fatalf("runs = %d", n)
	}
}

func TestSendNeedsCableAndBudget(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	if _, err := f.eng.Send(ctx, f.a.ID, f.b.ID, "hi", KindManual); err == nil {
		t.Fatal("send without a cable must fail")
	}
	f.link(t, types.SessionLink{})
	f.eng.AgentBudget = 1
	if _, err := f.eng.Send(ctx, f.a.ID, f.b.ID, "logo değişti", KindAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.Send(ctx, f.a.ID, f.b.ID, "yine", KindAgent); err == nil {
		t.Fatal("agent budget not enforced")
	}
	f.eng.Wait()
	calls := f.run.Calls()
	if len(calls) != 1 || !strings.Contains(calls[0].UserMessage, "logo değişti") || calls[0].HistoryLimit != 10 {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestSignalsAndCompactDiff(t *testing.T) {
	diff := `diff --git a/src/Header.tsx b/src/Header.tsx
index 1..2 100644
--- a/src/Header.tsx
+++ b/src/Header.tsx
@@ -1,3 +1,3 @@
 unchanged context line
-  <img src="/logo-old.svg" alt="Rove" />
+  <img src="/brand/logo-blue.svg" style={{ color: "#1a73e8" }} />
+  const primaryColor = BRAND_PRIMARY;
+  return value;
`
	sig := Signals(diff, 10)
	joined := strings.Join(sig, "|")
	for _, want := range []string{"#1a73e8", "/brand/logo-blue.svg", "BRAND_PRIMARY", "primaryColor"} {
		if !strings.Contains(joined, want) {
			t.Errorf("signals %v missing %s", sig, want)
		}
	}
	if strings.Contains(joined, "return") || strings.Contains(joined, "value") {
		t.Errorf("common words leaked: %v", sig)
	}
	c := compactDiff(diff, 1500)
	if strings.Contains(c, "unchanged context") || strings.Contains(c, "@@") || !strings.HasPrefix(c, "# src/Header.tsx") {
		t.Errorf("compact diff:\n%s", c)
	}
	if short := compactDiff(diff, 60); !strings.Contains(short, "kısaltıldı") || len([]rune(short)) > 90 {
		t.Errorf("budget not enforced: %q", short)
	}
}
