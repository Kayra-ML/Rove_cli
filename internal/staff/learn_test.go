package staff

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Kayra-ML/rove/internal/agent"
	"github.com/Kayra-ML/rove/internal/types"
)

// After a task the agent keeps what it learned as notes of its own, marked
// as learned; nothing when nothing was learned; and too many are folded,
// leaving the user's own notes alone.
func TestAgentsLearnFromTheirTasks(t *testing.T) {
	e, st := setup(t)
	ctx := context.Background()
	ali := hire(t, st, "Ali", "Backend geliştirici")
	e.Run = func(context.Context, agent.RunRequest) (agent.RunResult, error) {
		return agent.RunResult{Assistant: "Bitti; testler go test ./... ile geçti.", Done: true}, nil
	}
	var mu sync.Mutex
	var asked []string
	answer := "- Testler `go test ./...` ile çalışır\n- Ödeme kodu internal/pay altında\n- üçüncüsü fazla"
	e.Ask = func(_ context.Context, _ types.ID, system, user string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, user)
		if strings.Contains(system, "Merge") {
			var b strings.Builder
			for i := 0; i < 5; i++ {
				fmt.Fprintf(&b, "- birleşik %d\n", i)
			}
			return b.String(), nil
		}
		return answer, nil
	}
	_, _ = e.AddNote(ctx, ali.ID, "Kullanıcı kısa cevap ister") // the user's own

	task, _ := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "Ödeme hatasını düzelt"})
	waitFor(t, func() bool {
		n, _ := e.Notes(ctx, ali.ID)
		return len(n) == 3
	})
	notes, _ := e.Notes(ctx, ali.ID)
	learned := 0
	for _, n := range notes {
		if strings.HasPrefix(n.Key, LearnedPrefix) {
			learned++
		}
	}
	if learned != 2 {
		t.Fatalf("learned %d notes, want 2 (at most two a task): %+v", learned, notes)
	}
	mu.Lock()
	if !strings.Contains(asked[0], "Ödeme hatasını düzelt") || !strings.Contains(asked[0], "Kullanıcı kısa cevap ister") {
		t.Fatalf("the look back did not see the task and the notes:\n%s", asked[0])
	}
	mu.Unlock()
	_ = task

	// nothing learned: nothing kept
	mu.Lock()
	answer = "NONE"
	mu.Unlock()
	t2, _ := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "README düzelt"})
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(asked) == 2 })
	waitFor(t, func() bool { x, _ := e.Get(ctx, t2.ID); return x.Status == StatusDone })
	if n, _ := e.Notes(ctx, ali.ID); len(n) != 3 {
		t.Fatalf("a NONE kept notes: %d", len(n))
	}

	// too many learned notes are folded; the user's own stays
	for i := 0; i < learnedMax; i++ {
		_, _ = e.addLearned(ctx, ali.ID, fmt.Sprintf("ders numara %d", i))
	}
	mu.Lock()
	answer = "- yeni bir ders"
	mu.Unlock()
	t3, _ := e.Assign(ctx, AssignOpts{ProfileID: ali.ID, Brief: "Bir iş daha"})
	waitFor(t, func() bool {
		n, _ := e.Notes(ctx, ali.ID)
		return len(n) == 6 // the user's note and five merged ones
	})
	_ = t3
	notes, _ = e.Notes(ctx, ali.ID)
	mine := false
	for _, n := range notes {
		if n.Content == "Kullanıcı kısa cevap ister" && !strings.HasPrefix(n.Key, LearnedPrefix) {
			mine = true
		}
	}
	if !mine {
		t.Fatalf("folding touched the user's own note: %+v", notes)
	}
}

func TestParseLines(t *testing.T) {
	if got := parseLines("NONE.", 2); got != nil {
		t.Fatalf("NONE = %v", got)
	}
	if got := parseLines("Here:\n- a\n* b\n- c", 2); strings.Join(got, ",") != "a,b" {
		t.Fatalf("lines = %v", got)
	}
}
