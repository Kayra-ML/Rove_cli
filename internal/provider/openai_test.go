package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ssePayload builds a minimal SSE data line for test responses.
func ssePayload(content string, done bool) []byte {
	if done {
		return []byte("data: [DONE]\n\n")
	}
	chunk := map[string]any{
		"choices": []map[string]any{
			{"delta": map[string]any{"role": "assistant", "content": content}},
		},
	}
	b, _ := json.Marshal(chunk)
	return append([]byte("data: "), append(b, '\n', '\n')...)
}

func TestOpenAICompat_StreamsChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl := w.(http.Flusher)
		for _, tok := range []string{"Hello", " world"} {
			_, _ = w.Write(ssePayload(tok, false))
			fl.Flush()
		}
		_, _ = w.Write(ssePayload("", true))
		fl.Flush()
	}))
	defer srv.Close()

	p := &OpenAICompat{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
		Keys:       func(string) (string, error) { return "test-key", nil },
		SecretID:   "sk",
	}

	req := ChatRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: "hi"},
		},
	}
	ch, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for c := range ch {
		if !c.Done {
			got += c.Content
		}
	}
	if got != "Hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestOpenAICompat_HTTPErrorPropagated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"rate limit"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := &OpenAICompat{BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, err := p.Complete(context.Background(), ChatRequest{
		Model:    "gpt-4o",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 429")
	}
}

func TestOpenAICompat_RegisterInRouter(t *testing.T) {
	r := NewRouter()
	r.Register("openai", &OpenAICompat{BaseURL: "https://api.openai.com/v1"})
	c, _, err := r.Resolve("", "openai", "gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("nil completer")
	}
}