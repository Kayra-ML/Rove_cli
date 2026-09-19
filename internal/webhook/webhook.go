// Package webhook provides GitHub-style HMAC-SHA256 webhook verification
// and trigger logic for Rove Code background agents.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/id"
	"github.com/Kayra-ML/rove/internal/types"
)

// VerifySignature validates a GitHub-style X-Hub-Signature-256 header.
// sig must be in the form "sha256=<hex>".
func VerifySignature(body []byte, secret, sig string) bool {
	sig = strings.TrimPrefix(sig, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

// Store is the persistence interface required by the webhook engine.
type Store interface {
	UpsertWebhookRule(ctx context.Context, r types.WebhookRule) error
	ListWebhookRules(ctx context.Context) ([]types.WebhookRule, error)
	DeleteWebhookRule(ctx context.Context, id types.ID) error
}

// Engine handles incoming webhook payloads and triggers agents.
type Engine struct {
	store Store
}

// New creates a new webhook Engine.
func New(s Store) *Engine { return &Engine{store: s} }

// List returns all stored webhook rules.
func (e *Engine) List(ctx context.Context) ([]types.WebhookRule, error) {
	return e.store.ListWebhookRules(ctx)
}

// Upsert saves a webhook rule, assigning an ID if missing.
func (e *Engine) Upsert(ctx context.Context, r types.WebhookRule) (types.WebhookRule, error) {
	if r.ID == "" {
		r.ID = id.NewID()
	}
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	return r, e.store.UpsertWebhookRule(ctx, r)
}

// Delete removes a webhook rule by ID.
func (e *Engine) Delete(ctx context.Context, ruleID types.ID) error {
	return e.store.DeleteWebhookRule(ctx, ruleID)
}

// TriggerResult holds the outcome of processing an incoming webhook.
type TriggerResult struct {
	RuleID    types.ID `json:"ruleId"`
	RuleName  string   `json:"ruleName"`
	AgentID   types.ID `json:"agentId"`
	Triggered bool     `json:"triggered"`
	Message   string   `json:"message"`
}

// Process verifies the payload against all enabled rules that match eventType
// and returns the list of rules that fired. Callers are responsible for
// actually running the agents.
func (e *Engine) Process(ctx context.Context, eventType string, body []byte, sig string) ([]TriggerResult, error) {
	rules, err := e.store.ListWebhookRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("webhook: list rules: %w", err)
	}

	// Parse body to extract a human-readable summary for the agent prompt.
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)

	var results []TriggerResult
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if r.EventType != "*" && r.EventType != eventType {
			continue
		}
		// Verify HMAC signature if a secret is configured.
		// When a secret is set the signature header is REQUIRED — an absent or
		// blank sig is treated as a verification failure so that an attacker
		// cannot bypass HMAC by simply omitting the header.
		if r.Secret != "" {
			if sig == "" || !VerifySignature(body, r.Secret, sig) {
				results = append(results, TriggerResult{
					RuleID:    r.ID,
					RuleName:  r.Name,
					AgentID:   r.AgentID,
					Triggered: false,
					Message:   "signature missing or mismatch",
				})
				continue
			}
		}
		results = append(results, TriggerResult{
			RuleID:    r.ID,
			RuleName:  r.Name,
			AgentID:   r.AgentID,
			Triggered: true,
			Message:   fmt.Sprintf("webhook %s fired rule %q", eventType, r.Name),
		})
	}
	return results, nil
}
