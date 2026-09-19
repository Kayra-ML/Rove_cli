package billing

import (
	"strings"
	"testing"
)

func TestEstimateKnownModel(t *testing.T) {
	// claude-sonnet-4-6: $3 input / $15 output per 1M tokens
	cost := Estimate("anthropic", "claude-sonnet-4-6", 1_000_000, 1_000_000)
	if cost != 18.0 {
		t.Fatalf("cost = %v want 18.0", cost)
	}
}

func TestEstimateUnknownModelUsesDefault(t *testing.T) {
	// Default: $1 input / $3 output per 1M tokens
	cost := Estimate("unknown", "unknown-model", 1_000_000, 1_000_000)
	if cost != 4.0 {
		t.Fatalf("cost = %v want 4.0", cost)
	}
}

func TestEstimateZeroTokens(t *testing.T) {
	cost := Estimate("openai", "gpt-4o", 0, 0)
	if cost != 0 {
		t.Fatalf("cost = %v want 0", cost)
	}
}

func TestEstimateSmallUsage(t *testing.T) {
	// 100 input + 200 output tokens for gpt-4o-mini
	// $0.15 / 1M input, $0.60 / 1M output
	cost := Estimate("openai", "gpt-4o-mini", 100, 200)
	expected := 0.15*100.0/1_000_000 + 0.60*200.0/1_000_000
	if cost != expected {
		t.Fatalf("cost = %v want %v", cost, expected)
	}
}

func TestAllPricesNonEmpty(t *testing.T) {
	prices := AllPrices()
	if len(prices) == 0 {
		t.Fatal("AllPrices returned empty")
	}
}

func TestAllPricesKeysHaveSlash(t *testing.T) {
	for _, p := range AllPrices() {
		if !strings.Contains(p.Key, "/") {
			t.Fatalf("price key %q has no slash", p.Key)
		}
		if p.InputPer1M <= 0 || p.OutputPer1M <= 0 {
			t.Fatalf("price key %q has non-positive rate: in=%v out=%v", p.Key, p.InputPer1M, p.OutputPer1M)
		}
	}
}

func TestPriceTableCoversMajorModels(t *testing.T) {
	required := []string{
		"anthropic/claude-sonnet-4-6",
		"openai/gpt-4o",
		"google/gemini-2.5-pro",
		"xai/grok-4",
		"deepseek/deepseek-r1",
	}
	for _, key := range required {
		if _, ok := PriceTable[key]; !ok {
			t.Fatalf("PriceTable missing %q", key)
		}
	}
}