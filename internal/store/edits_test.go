package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

// The light queries the terminal board uses agree with the full ones.
func TestLightQueriesMatchFullOnes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if got, _ := s.LastUserMessage(ctx, "S"); got != "" {
		t.Fatalf("empty session: %q", got)
	}
	at := time.Now().UTC()
	if err := s.UpsertAgent(ctx, types.Agent{ID: "a", Name: "a", Provider: "fake", Model: "m", CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSession(ctx, types.Session{ID: "S", AgentID: "a", CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	for i, m := range []types.Message{
		{ID: "1", Role: types.RoleUser, Content: "ilk"},
		{ID: "2", Role: types.RoleAssistant, Content: "cevap"},
		{ID: "3", Role: types.RoleUser, Content: "son iş", Images: []string{"data:image/png;base64,AAAA"}},
		{ID: "4", Role: types.RoleAssistant, Content: "tamam"},
	} {
		m.SessionID, m.CreatedAt = "S", at.Add(time.Duration(i)*time.Second)
		if err := s.InsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.LastUserMessage(ctx, "S"); got != "son iş" {
		t.Fatalf("last user message = %q", got)
	}
	for i, p := range []string{"b.go", "a.go", "c.go"} {
		run := types.ID("r1")
		if i == 2 {
			run = "r2"
		}
		if _, err := s.PutTurnEdit(ctx, types.TurnEdit{RunID: run, SessionID: "S", Path: p, Before: "big content", CreatedAt: at.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, run := range []types.ID{"", "r1", "r2", "none"} {
		full, _ := s.TurnEdits(ctx, "S", run)
		var want []string
		for _, e := range full {
			want = append(want, e.Path)
		}
		got, _ := s.TurnEditPaths(ctx, "S", run)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %q: paths %v, want %v", run, got, want)
		}
	}
}
