package persona

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now().UTC()
	_ = st.UpsertSession(context.Background(), types.Session{ID: "s1", Title: "x", CreatedAt: now, UpdatedAt: now})
	return st
}

func TestCatalogIsConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Characters {
		if seen[c.ID] {
			t.Errorf("duplicate character %s", c.ID)
		}
		seen[c.ID] = true
		if c.Name == "" || c.Summary == "" || len(c.Prompt) < 300 {
			t.Errorf("%s is incomplete", c.ID)
		}
		if !strings.Contains(c.Prompt, langRule) {
			t.Errorf("%s does not answer in the user's language", c.ID)
		}
		if tk := tokens(c.Prompt); tk > 320 {
			t.Errorf("%s prompt is %d tokens; keep characters lean", c.ID, tk)
		}
		for _, f := range c.Features {
			if _, ok := FeatureByKey(f); !ok {
				t.Errorf("%s uses unknown feature %s", c.ID, f)
			}
		}
	}
	if len(Characters) < 12 {
		t.Errorf("catalog too small: %d", len(Characters))
	}
}

func TestResolvePrecedence(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)

	r, err := Resolve(ctx, st, "s1")
	if err != nil || r.Source != "none" || r.Features != nil || r.Prompt != "" {
		t.Fatalf("empty = %+v %v", r, err)
	}
	if !r.AllowTool("shell") {
		t.Fatal("no persona must allow every tool")
	}

	// default profile applies to sessions without their own choice
	_, _ = st.UpsertAgentProfile(ctx, types.AgentProfile{ID: "p1", Name: "Sec", CharacterID: "security", SystemPrompt: "Focus on the auth module.", IsDefault: true, Model: "m1"})
	r, _ = Resolve(ctx, st, "s1")
	if r.Source != "default-profile" || r.CharacterID != "security" || r.Model != "m1" {
		t.Fatalf("default profile = %+v", r)
	}
	if !strings.Contains(r.Prompt, "application security engineer") || !strings.Contains(r.Prompt, "auth module") {
		t.Fatalf("profile prompt = %q", r.Prompt)
	}
	if r.AllowTool("write_file") {
		t.Fatal("security character has no write feature")
	}

	// the session's own character wins, with feature overrides and rules
	_ = st.PutSessionPersona(ctx, types.SessionPersona{SessionID: "s1", CharacterID: "frontend", Features: []string{"read", "concise"}, ExtraPrompt: "Use Tailwind."})
	r, _ = Resolve(ctx, st, "s1")
	if r.Source != "character" || r.CharacterID != "frontend" {
		t.Fatalf("character = %+v", r)
	}
	if !r.AllowTool("read_file") || r.AllowTool("shell") || r.AllowTool("mcp_github_search") {
		t.Fatal("feature gate wrong")
	}
	if !r.AllowTool("some_future_tool") {
		t.Fatal("unknown tools must stay available")
	}
	if !strings.Contains(r.Prompt, "Working rules:\n- Be terse") || !strings.HasSuffix(r.Prompt, "Use Tailwind.") {
		t.Fatalf("composed prompt = %q", r.Prompt)
	}

	// an explicit empty feature list turns every tool off
	_ = st.PutSessionPersona(ctx, types.SessionPersona{SessionID: "s1", Features: []string{}})
	r, _ = Resolve(ctx, st, "s1")
	if r.Source != "features" || r.AllowTool("read_file") {
		t.Fatalf("all-off = %+v", r)
	}
}

func TestCostCountsOnlyEnabledTools(t *testing.T) {
	specs := []provider.ToolSpec{
		{Name: "read_file", Description: "Read a file.", Parameters: json.RawMessage(`{"type":"object"}`)},
		{Name: "shell", Description: strings.Repeat("x", 400), Parameters: json.RawMessage(`{}`)},
	}
	all := Resolved{}.Cost(specs)
	lean := Resolved{Features: []string{"read"}, Prompt: "hi"}.Cost(specs)
	if lean.ToolsOff != 1 || lean.Tools >= all.Tools || lean.Total != lean.Prompt+lean.Tools {
		t.Fatalf("all=%+v lean=%+v", all, lean)
	}
}

func TestMatch(t *testing.T) {
	for q, want := range map[string]string{"frontend": "frontend", "go": "go-backend", "güvenlik": "security", "react": "frontend", "Teknik Yazar": "writer"} {
		if c, ok := Match(q); !ok || c.ID != want {
			t.Errorf("Match(%q) = %s, want %s", q, c.ID, want)
		}
	}
	if _, ok := Match("zzz"); ok {
		t.Error("nonsense matched")
	}
}

// An office agent can keep its character's permissions but speak with a
// prompt of the user's own.
func TestProfileOwnPrompt(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	_, _ = st.UpsertAgentProfile(ctx, types.AgentProfile{ID: "p2", Name: "Mine", CharacterID: "security", SystemPrompt: "You review only Go code.", PromptMode: types.PromptOwn, Mark: "hex:visor", Color: "#3fb0a0"})
	_ = st.PutSessionPersona(ctx, types.SessionPersona{SessionID: "s1", ProfileID: "p2"})
	r, _ := Resolve(ctx, st, "s1")
	if strings.Contains(r.Prompt, "application security engineer") || !strings.HasPrefix(r.Prompt, "You review only Go code.") {
		t.Fatalf("own prompt = %q", r.Prompt)
	}
	if r.AllowTool("write_file") {
		t.Fatal("the character's permissions should still apply")
	}
	p, _ := st.GetAgentProfile(ctx, "p2")
	if p.Mark != "hex:visor" || p.PromptMode != types.PromptOwn || p.Color != "#3fb0a0" {
		t.Fatalf("stored = %+v", p)
	}
}

func TestAllowIntegration(t *testing.T) {
	all := Resolved{}
	if !all.AllowIntegration("mcp_sentry_issues") {
		t.Fatal("no list should allow every server")
	}
	gh := Resolved{Integrations: []string{"github"}}
	if !gh.AllowIntegration("mcp_github_list_prs") || gh.AllowIntegration("mcp_sentry_issues") {
		t.Fatal("a picked list should allow only its servers")
	}
	if !gh.AllowIntegration("read_file") {
		t.Fatal("non-MCP tools are not integrations")
	}
	// a server whose name starts like another's is not it
	if gh.AllowIntegration("mcp_githubenterprise_x") {
		t.Fatal("prefix match leaked to another server")
	}
	if (Resolved{Integrations: []string{}}).AllowIntegration("mcp_github_x") {
		t.Fatal("an empty list should allow none")
	}
}
