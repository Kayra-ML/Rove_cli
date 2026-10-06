package store

import (
	"sort"
	"testing"
	"time"
)

// Timestamps are compared as text in ORDER BY: their text order must be
// their time order, down to the nanosecond.
func TestTimestampsSortAsText(t *testing.T) {
	base := time.Date(2026, 9, 27, 12, 0, 5, 0, time.UTC)
	times := []time.Time{base, base.Add(500 * time.Millisecond), base.Add(510 * time.Millisecond), base.Add(time.Nanosecond), base.Add(time.Second)}
	var texts []string
	for _, x := range times {
		texts = append(texts, ts(x))
	}
	sorted := append([]string(nil), texts...)
	sort.Strings(sorted)
	want := []string{texts[0], texts[3], texts[1], texts[2], texts[4]}
	for i := range want {
		if sorted[i] != want[i] {
			t.Fatalf("text order %v, want %v", sorted, want)
		}
	}
	for i, x := range times {
		if got := parseTS(texts[i]); !got.Equal(x) {
			t.Fatalf("round trip %v -> %v", x, got)
		}
	}
	// rows written before the fixed width still parse
	if got := parseTS("2026-09-27T12:00:05.5Z"); !got.Equal(base.Add(500 * time.Millisecond)) {
		t.Fatalf("old format = %v", got)
	}
}
