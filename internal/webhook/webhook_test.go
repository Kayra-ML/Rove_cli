package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/aether-dev/aether/internal/types"
)

type memStore struct {
	rules []types.WebhookRule
}

func (m *memStore) UpsertWebhookRule(_ context.Context, r types.WebhookRule) error {
	for i, existing := range m.rules {
		if existing.ID == r.ID {
			m.rules[i] = r
			return nil
		}
	}
	m.rules = append(m.rules, r)
	return nil
}

func (m *memStore) ListWebhookRules(_ context.Context) ([]types.WebhookRule, error) {
	return m.rules, nil
}

func (m *memStore) DeleteWebhookRule(_ context.Context, id types.ID) error {
	out := m.rules[:0]
	for _, r := range m.rules {
		if r.ID != id {
			out = append(out, r)
		}
	}
	m.rules = out
	return nil
}

// --- helpers ---

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newEngine() (*Engine, *memStore) {
	s := &memStore{}
	return New(s), s
}

// --- VerifySignature ---

func TestVerifySignatureValid(t *testing.T) {
	body := []byte(`{"action":"push"}`)
	secret := "supersecret"
	sig := sign(body, secret)
	if !VerifySignature(body, secret, sig) {
		t.Fatal("expected valid signature")
	}
}

func TestVerifySignatureInvalidSecret(t *testing.T) {
	body := []byte(`{"action":"push"}`)
	sig := sign(body, "correct-secret")
	if VerifySignature(body, "wrong-secret", sig) {
		t.Fatal("expected invalid signature")
	}
}

func TestVerifySignatureMissingPrefix(t *testing.T) {
	body := []byte(`{"x":1}`)
	secret := "s"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	raw := hex.EncodeToString(mac.Sum(nil)) // no "sha256=" prefix
	if !VerifySignature(body, secret, raw) {
		t.Fatal("should accept sig without sha256= prefix via TrimPrefix no-op")
	}
}

func TestVerifySignatureEmptySig(t *testing.T) {
	if VerifySignature([]byte("x"), "secret", "") {
		t.Fatal("empty sig must fail")
	}
}

// --- Upsert / List / Delete ---

func TestUpsertAssignsID(t *testing.T) {
	e, _ := newEngine()
	r, err := e.Upsert(context.Background(), types.WebhookRule{
		Name:      "test",
		EventType: "push",
		Enabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" {
		t.Fatal("expected non-empty ID")
	}
}

func TestUpsertPreservesID(t *testing.T) {
	e, _ := newEngine()
	r, _ := e.Upsert(context.Background(), types.WebhookRule{
		ID: "custom-id", Name: "test", EventType: "push", Enabled: true,
	})
	if r.ID != "custom-id" {
		t.Fatalf("want custom-id got %s", r.ID)
	}
}

func TestListEmpty(t *testing.T) {
	e, _ := newEngine()
	rules, err := e.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("want 0 got %d", len(rules))
	}
}

func TestDeleteRemovesRule(t *testing.T) {
	e, _ := newEngine()
	r, _ := e.Upsert(context.Background(), types.WebhookRule{
		Name: "del", EventType: "*", Enabled: true,
	})
	if err := e.Delete(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	rules, _ := e.List(context.Background())
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules after delete, got %d", len(rules))
	}
}

// --- Process ---

func TestProcessFiresMatchingRule(t *testing.T) {
	e, _ := newEngine()
	body := []byte(`{"ref":"main"}`)
	secret := "mysecret"
	e.Upsert(context.Background(), types.WebhookRule{
		ID: "r1", Name: "push-rule", Secret: secret,
		EventType: "push", AgentID: "agent1", Enabled: true,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	results, err := e.Process(context.Background(), "push", body, sign(body, secret))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Triggered {
		t.Fatalf("expected 1 triggered result, got %+v", results)
	}
}

func TestProcessRejectsWrongSignature(t *testing.T) {
	e, _ := newEngine()
	body := []byte(`{"x":1}`)
	e.Upsert(context.Background(), types.WebhookRule{
		ID: "r1", Name: "secured", Secret: "correct",
		EventType: "push", Enabled: true,
	})

	results, _ := e.Process(context.Background(), "push", body, sign(body, "wrong"))
	if len(results) != 1 || results[0].Triggered {
		t.Fatalf("expected 1 not-triggered result, got %+v", results)
	}
}

func TestProcessSkipsEmptySigWhenSecretSet(t *testing.T) {
	e, _ := newEngine()
	e.Upsert(context.Background(), types.WebhookRule{
		ID: "r1", Name: "secured", Secret: "s",
		EventType: "push", Enabled: true,
	})
	results, _ := e.Process(context.Background(), "push", []byte(`{}`), "")
	if results[0].Triggered {
		t.Fatal("absent sig must not trigger a secured rule")
	}
}

func TestProcessNoSecretAlwaysFires(t *testing.T) {
	e, _ := newEngine()
	e.Upsert(context.Background(), types.WebhookRule{
		ID: "r1", Name: "open", Secret: "",
		EventType: "*", Enabled: true,
	})
	results, _ := e.Process(context.Background(), "anything", []byte(`{}`), "")
	if !results[0].Triggered {
		t.Fatal("rule with no secret should always fire")
	}
}

func TestProcessSkipsDisabledRule(t *testing.T) {
	e, _ := newEngine()
	e.Upsert(context.Background(), types.WebhookRule{
		ID: "r1", Name: "off", EventType: "*", Enabled: false,
	})
	results, _ := e.Process(context.Background(), "push", []byte(`{}`), "")
	if len(results) != 0 {
		t.Fatalf("disabled rule must not appear in results, got %+v", results)
	}
}

func TestProcessEventTypeWildcard(t *testing.T) {
	e, _ := newEngine()
	e.Upsert(context.Background(), types.WebhookRule{
		ID: "r1", Name: "all", EventType: "*", Enabled: true,
	})
	for _, ev := range []string{"push", "pr", "release", "custom"} {
		r, _ := e.Process(context.Background(), ev, []byte(`{}`), "")
		if len(r) == 0 || !r[0].Triggered {
			t.Fatalf("wildcard rule should fire for event %q", ev)
		}
	}
}

func TestProcessEventTypeMismatch(t *testing.T) {
	e, _ := newEngine()
	e.Upsert(context.Background(), types.WebhookRule{
		ID: "r1", Name: "pr-only", EventType: "pull_request", Enabled: true,
	})
	results, _ := e.Process(context.Background(), "push", []byte(`{}`), "")
	if len(results) != 0 {
		t.Fatalf("event type mismatch must not trigger, got %+v", results)
	}
}
