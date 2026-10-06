package usage

import (
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/store"
)

func TestBuildByDayAndModel(t *testing.T) {
	loc := time.FixedZone("TRT", 3*3600)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, loc)
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, loc).UTC() }
	entries := []store.UsageLedgerEntry{
		// 30 Sep, late at night in Turkey: still that day there, though UTC says otherwise
		{Provider: "anthropic", Model: "opus", SentTokens: 1000, RecvTokens: 200, Reported: true, PromptTokens: 1500, CompletionTokens: 250, CachedTokens: 400, CreatedAt: time.Date(2026, 9, 30, 23, 30, 0, 0, loc).UTC()},
		{Provider: "anthropic", Model: "opus", SentTokens: 500, RecvTokens: 100, CreatedAt: at(2, 9)},
		{Provider: "qwen", Model: "flash", SentTokens: 300, RecvTokens: 50, Reported: true, PromptTokens: 320, CompletionTokens: 40, CreatedAt: at(2, 10)},
		// older than the range: left out
		{Provider: "qwen", Model: "flash", SentTokens: 9999, CreatedAt: at(1, 0).AddDate(0, 0, -10)},
	}
	r := Build(entries, 3, now, loc)
	if r.Since != "2026-09-30" || r.Until != "2026-10-02" || len(r.Days) != 3 {
		t.Fatalf("range = %s..%s, %d days", r.Since, r.Until, len(r.Days))
	}
	// every day is there, the quiet one at zero
	if r.Days[0].Calls != 1 || r.Days[1].Calls != 0 || r.Days[2].Calls != 2 {
		t.Fatalf("days = %+v", r.Days)
	}
	if r.Total.SentTokens != 1800 || r.Total.RecvTokens != 350 || r.Total.Calls != 3 {
		t.Fatalf("total = %+v", r.Total)
	}
	// the providers' word only where they gave it, and ours over the same calls
	if r.Total.ReportedCalls != 2 || r.Total.ReportedIn != 1820 || r.Total.ReportedOut != 290 || r.Total.Cached != 400 || r.Total.OursOnReported != 1200+350 {
		t.Fatalf("reported = %+v", r.Total)
	}
	// models, biggest first
	if len(r.Models) != 2 || r.Models[0].Model != "opus" || r.Models[0].Calls != 2 || r.Models[1].Model != "flash" {
		t.Fatalf("models = %+v", r.Models)
	}
}
