package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Kayra-ML/rove/internal/eventbus"
	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

// recorder answers "ok" (or a summary when asked to compact) and keeps
// every request.
type recorder struct {
	mu   sync.Mutex
	reqs []provider.ChatRequest
}

func (*recorder) Kind() types.ProviderKind { return types.ProviderFake }
func (*recorder) Name() string             { return "rec" }
func (r *recorder) Complete(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	reply := "ok"
	if strings.Contains(req.Messages[0].Content, "You compact a coding chat") {
		reply = "ÖZET: kullanıcı giriş sayfası istiyor; login.go yazıldı."
	}
	ch := make(chan provider.ChatDelta, 2)
	ch <- provider.ChatDelta{Content: reply}
	ch <- provider.ChatDelta{Done: true}
	close(ch)
	return ch, nil
}

func (r *recorder) last() provider.ChatRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reqs[len(r.reqs)-1]
}

func ctxRig(t *testing.T, persona func(context.Context, types.ID) Persona) (*Runtime, *recorder, types.Agent, *session.Manager) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	r := provider.NewRouter()
	rec := &recorder{}
	r.Register("rec", rec)
	sess := session.New(s, bus)
	rt := New(s, bus, sess, nil, r, tool.New(nil))
	if persona != nil {
		rt.SetPersonaSource(persona)
	}
	ag, _ := rt.Upsert(context.Background(), types.Agent{Name: "a", Provider: "rec", Model: "m"})
	return rt, rec, ag, sess
}

func TestProjectRulesAreInTheSystemPrompt(t *testing.T) {
	rt, rec, ag, sess := ctxRig(t, nil)
	ctx := context.Background()
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("Always run `make test` before finishing."), 0o644)
	_ = os.WriteFile(filepath.Join(ws, ".cursorrules"), []byte("Use tabs."), 0o644)
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, Workspace: ws, UserMessage: "x"}); err != nil {
		t.Fatal(err)
	}
	sys := rec.last().Messages[0].Content
	if !strings.Contains(sys, "### AGENTS.md\nAlways run `make test`") || !strings.Contains(sys, "### .cursorrules\nUse tabs.") {
		t.Fatalf("system = %s", sys)
	}
	// rules come before the per-run parts (workspace line), for caching
	if strings.Index(sys, "## Project rules") > strings.Index(sys, "Workspace: ") {
		t.Fatal("rules should come before the workspace line")
	}
	// how agents write sits in the stable part too, before the rules
	if i := strings.Index(sys, "No emojis"); i < 0 || i > strings.Index(sys, "## Project rules") {
		t.Fatalf("style rule missing or after the rules: %s", sys)
	}
	// a changed file is read again; a big one is capped
	_ = os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte(strings.Repeat("x", 20000)), 0o644)
	text, files := ProjectRules(ws)
	if len(text) > rulesBudget+400 || len(files) == 0 || files[0] != "AGENTS.md" {
		t.Fatalf("rules = %d chars %v", len(text), files)
	}
	if text, _ := ProjectRules(t.TempDir()); text != "" {
		t.Fatal("no rule files should add nothing")
	}
}

func TestLongChatsAreSummarized(t *testing.T) {
	rt, rec, ag, sess := ctxRig(t, nil)
	rt.CompactAt, rt.KeepTurns = 500, 2
	ctx := context.Background()
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	for i := 0; i < 8; i++ {
		_, _ = sess.Append(ctx, types.Message{SessionID: s.ID, Role: types.RoleUser, Content: "eski soru " + strings.Repeat("a", 300)})
		_, _ = sess.Append(ctx, types.Message{SessionID: s.ID, Role: types.RoleAssistant, Content: "eski cevap " + strings.Repeat("b", 300)})
	}
	if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "yeni soru"}); err != nil {
		t.Fatal(err)
	}
	req := rec.last()
	if !strings.Contains(req.Messages[0].Content, "## Earlier in this conversation (summary)\nÖZET:") {
		t.Fatalf("summary not in the prompt: %s", req.Messages[0].Content)
	}
	users := 0
	for _, m := range req.Messages {
		if m.Role == "user" {
			users++
		}
	}
	if users != 2 { // the kept turns: the last old question and the new one
		t.Fatalf("sent %d user turns after compaction", users)
	}
	// the chat keeps every message for the user, with the summary marked
	hist, _ := sess.History(ctx, s.ID)
	summaries := 0
	for i, m := range hist {
		if m.Kind == types.MessageSummary {
			summaries++
			if hist[i+1].Role != types.RoleUser || !strings.HasPrefix(hist[i+1].Content, "eski soru") {
				t.Fatalf("summary is not placed before the first kept turn: %+v", hist[i+1])
			}
		}
	}
	if summaries != 1 || len(hist) != 8*2+1+2 {
		t.Fatalf("history = %d messages, %d summaries", len(hist), summaries)
	}
	// /compact works on demand even for a short chat
	s2, _ := sess.Create(ctx, "s2", ag.ID, "")
	_, _ = sess.Append(ctx, types.Message{SessionID: s2.ID, Role: types.RoleUser, Content: "bir"})
	_, _ = sess.Append(ctx, types.Message{SessionID: s2.ID, Role: types.RoleAssistant, Content: "iki"})
	_, _ = sess.Append(ctx, types.Message{SessionID: s2.ID, Role: types.RoleUser, Content: "üç"})
	if ok, err := rt.Compact(ctx, s2.ID, ag.ID); err != nil || !ok {
		t.Fatalf("compact = %v %v", ok, err)
	}
}

func TestImagesEffortAndCacheKey(t *testing.T) {
	rt, rec, ag, sess := ctxRig(t, func(context.Context, types.ID) Persona { return Persona{Effort: "high"} })
	ctx := context.Background()
	s, _ := sess.Create(ctx, "s", ag.ID, "")
	img := "data:image/png;base64,AAAA"
	for i := 0; i < 3; i++ {
		if _, err := rt.Run(ctx, RunRequest{AgentID: ag.ID, SessionID: s.ID, UserMessage: "bak", Images: []string{img}}); err != nil {
			t.Fatal(err)
		}
	}
	req := rec.last()
	if req.ReasoningEffort != "high" || req.CacheKey != string(s.ID) {
		t.Fatalf("effort %q cache %q", req.ReasoningEffort, req.CacheKey)
	}
	var withImg, named int
	for _, m := range req.Messages {
		if len(m.Images) > 0 {
			withImg++
		}
		if strings.Contains(m.Content, "image(s) attached earlier") {
			named++
		}
	}
	if withImg != 2 || named != 1 {
		t.Fatalf("images re-sent in %d turns, named in %d (want 2 and 1)", withImg, named)
	}
	hist, _ := sess.History(ctx, s.ID)
	if len(hist[0].Images) != 1 {
		t.Fatal("the image was not kept with the message")
	}
}
