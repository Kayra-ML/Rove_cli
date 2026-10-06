package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/session"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

// claims answers and reports a usage of its own choosing.
type claims struct{ in, out int }

func (claims) Kind() types.ProviderKind { return types.ProviderFake }
func (claims) Name() string             { return "claims" }
func (c claims) Complete(context.Context, provider.ChatRequest) (<-chan provider.ChatDelta, error) {
	ch := make(chan provider.ChatDelta, 2)
	ch <- provider.ChatDelta{Content: "Merhaba, işi bitirdim."}
	ch <- provider.ChatDelta{Done: true, Usage: &provider.Usage{PromptTokens: c.in, CompletionTokens: c.out}}
	close(ch)
	return ch, nil
}

// Every call is in the ledger twice over: what Rove itself sent and got
// back, counted from the text, and what the provider says it cost.
func TestEveryCallIsCountedByRoveAndByTheProvider(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sess := session.New(s, nil)
	r := provider.NewRouter()
	r.Register("claims", claims{in: 90000, out: 5000})
	r.Register("fake", &provider.Fake{Responses: []string{"tamam"}})
	rt := New(s, nil, sess, nil, r, tool.New(nil))
	ctx := context.Background()
	big, _ := rt.Upsert(ctx, types.Agent{Name: "c", Provider: "claims", Model: "m1", Profile: "c"})
	quiet, _ := rt.Upsert(ctx, types.Agent{Name: "f", Provider: "fake", Model: "fake", Profile: "f"})
	chat, _ := sess.Create(ctx, "s", big.ID, "")
	if _, err := rt.Run(ctx, RunRequest{AgentID: big.ID, SessionID: chat.ID, UserMessage: "selam"}); err != nil {
		t.Fatal(err)
	}
	other, _ := sess.Create(ctx, "s2", quiet.ID, "")
	if _, err := rt.Run(ctx, RunRequest{AgentID: quiet.ID, SessionID: other.ID, UserMessage: "selam"}); err != nil {
		t.Fatal(err)
	}

	rows, err := s.ListUsageSince(ctx, time.Now().Add(-time.Hour))
	if err != nil || len(rows) != 2 {
		t.Fatalf("ledger = %+v, %v", rows, err)
	}
	c := rows[0]
	if c.Provider != "claims" || c.Model != "m1" || c.Kind != "chat" || c.SessionID != string(chat.ID) {
		t.Fatalf("row = %+v", c)
	}
	// the provider's word is kept as it was given…
	if !c.Reported || c.PromptTokens != 90000 || c.CompletionTokens != 5000 {
		t.Fatalf("reported = %+v", c)
	}
	// …and Rove's own count sits beside it: the reply is exactly what came back
	if c.RecvChars != len([]rune("Merhaba, işi bitirdim.")) || c.RecvTokens == 0 || c.SentTokens == 0 || c.SentChars == 0 {
		t.Fatalf("measured = %+v", c)
	}
	if c.SentTokens > 5000 {
		t.Fatalf("a short chat is not %d tokens", c.SentTokens)
	}
	// a provider that reports nothing still leaves Rove's count
	if q := rows[1]; q.Reported || q.PromptTokens != 0 || q.SentTokens == 0 || q.RecvChars != len("tamam") {
		t.Fatalf("unreported = %+v", q)
	}
}
