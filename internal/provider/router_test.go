package provider

import (
	"context"
	"strings"
	"testing"
)

func TestFakeComplete_StreamsAndDone(t *testing.T) {
	f := &Fake{Responses: []string{"hello world from fake"}}
	ch, err := f.Complete(context.Background(), ChatRequest{Model: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	done := false
	for d := range ch {
		b.WriteString(d.Content)
		if d.Done {
			done = true
		}
	}
	if b.String() != "hello world from fake" {
		t.Fatalf("got %q", b.String())
	}
	if !done {
		t.Fatal("missing done")
	}
}

func TestRouterResolve(t *testing.T) {
	r := NewRouter()
	r.Register("fake", &Fake{NameVal: "fake", Responses: []string{"ok"}})
	r.SetDefault("lead", "fake")
	c, model, err := r.Resolve("lead", "", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name() != "fake" || model != "m1" {
		t.Fatalf("%s %s", c.Name(), model)
	}
	if _, err := r.Get("missing"); err == nil {
		t.Fatal("expected error")
	}
}

func TestCancelStopsStream(t *testing.T) {
	f := &Fake{Responses: []string{strings.Repeat("x", 1000)}}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := f.Complete(ctx, ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	for range ch {
	}
}

func TestMeterAddLoadSnapshot(t *testing.T) {
	var m Meter
	m.Add(Usage{PromptTokens: 10, CompletionTokens: 20})
	m.Add(Usage{})
	m.Add(Usage{PromptTokens: 5, CompletionTokens: 5})
	s := m.Snapshot()
	if s.PromptTokens != 15 || s.CompletionTokens != 25 || s.TotalTokens != 40 || s.Calls != 2 {
		t.Fatalf("%+v", s)
	}
	var m2 Meter
	m2.Load(s)
	got := m2.Snapshot()
	if got != s {
		t.Fatalf("load %+v != %+v", got, s)
	}
}
