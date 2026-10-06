// Package usage turns the usage ledger into what Settings → Usage shows:
// tokens per day and per model, by Rove's own count of the text that went
// out and came back, and — beside it, never instead of it — what the
// providers reported for the same calls.
package usage

import (
	"sort"
	"time"

	"github.com/Kayra-ML/rove/internal/store"
)

// Tally is a sum of calls.
type Tally struct {
	Calls int `json:"calls"`
	// Rove's own count: what it sent and what came back.
	SentTokens int `json:"sentTokens"`
	RecvTokens int `json:"recvTokens"`
	SentChars  int `json:"sentChars"`
	RecvChars  int `json:"recvChars"`
	// What the providers reported, for the calls they reported on.
	ReportedCalls int `json:"reportedCalls"`
	ReportedIn    int `json:"reportedIn"`
	ReportedOut   int `json:"reportedOut"`
	Cached        int `json:"cached"`
	// Rove's count over the same reported calls, so the two compare like
	// with like.
	OursOnReported int `json:"oursOnReported"`
}

func (t *Tally) add(e store.UsageLedgerEntry) {
	t.Calls++
	t.SentTokens += e.SentTokens
	t.RecvTokens += e.RecvTokens
	t.SentChars += e.SentChars
	t.RecvChars += e.RecvChars
	if e.Reported {
		t.ReportedCalls++
		t.ReportedIn += e.PromptTokens
		t.ReportedOut += e.CompletionTokens
		t.Cached += e.CachedTokens
		t.OursOnReported += e.SentTokens + e.RecvTokens
	}
}

type Day struct {
	Date string `json:"date"` // YYYY-MM-DD, local time
	Tally
}

type Model struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Tally
}

type Report struct {
	Days   []Day   `json:"days"`
	Models []Model `json:"models"`
	Total  Tally   `json:"total"`
	// Since and Until bound the range, local dates.
	Since string `json:"since"`
	Until string `json:"until"`
}

// Build sums entries into the last `days` days ending today in loc. Every
// day of the range is present, the quiet ones at zero, so a chart of it
// shows the gaps.
func Build(entries []store.UsageLedgerEntry, days int, now time.Time, loc *time.Location) Report {
	if days < 1 {
		days = 1
	}
	today := now.In(loc)
	first := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	r := Report{Since: first.Format("2006-01-02"), Until: today.Format("2006-01-02")}
	index := map[string]int{}
	for d := 0; d < days; d++ {
		date := first.AddDate(0, 0, d).Format("2006-01-02")
		index[date] = len(r.Days)
		r.Days = append(r.Days, Day{Date: date})
	}
	models := map[[2]string]*Model{}
	for _, e := range entries {
		date := e.CreatedAt.In(loc).Format("2006-01-02")
		i, ok := index[date]
		if !ok {
			continue
		}
		r.Days[i].add(e)
		r.Total.add(e)
		k := [2]string{e.Provider, e.Model}
		m := models[k]
		if m == nil {
			m = &Model{Provider: e.Provider, Model: e.Model}
			models[k] = m
		}
		m.add(e)
	}
	for _, m := range models {
		r.Models = append(r.Models, *m)
	}
	sort.Slice(r.Models, func(i, j int) bool {
		a, b := r.Models[i], r.Models[j]
		if x, y := a.SentTokens+a.RecvTokens, b.SentTokens+b.RecvTokens; x != y {
			return x > y
		}
		return a.Provider+a.Model < b.Provider+b.Model
	})
	if r.Models == nil {
		r.Models = []Model{}
	}
	return r
}

// Start is where a range of days begins, for the ledger query.
func Start(days int, now time.Time, loc *time.Location) time.Time {
	if days < 1 {
		days = 1
	}
	t := now.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
}
