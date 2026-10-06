package provider

import (
	"context"
	"encoding/json"
	"io"
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

// A tool turn is not streamed: a gateway that answers a streamed tool
// request with the call written out as text would otherwise never run it.
func TestToolTurnIsNotStreamed(t *testing.T) {
	var streamed *bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s, _ := body["stream"].(bool)
		streamed = &s
		if _, ok := body["stream_options"]; ok {
			t.Errorf("a non-streamed request must not carry stream_options")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"bakıyorum","tool_calls":[{"id":"c1","type":"function","function":{"name":"shell","arguments":"{\"command\":\"hostname\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":40,"completion_tokens":9,"prompt_tokens_details":{"cached_tokens":12}}}`)
	}))
	defer srv.Close()

	p := &OpenAICompat{BaseURL: srv.URL, HTTPClient: srv.Client()}
	ch, err := p.Complete(context.Background(), ChatRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "user", Content: "hostname çalıştır"}},
		Tools:    []ToolSpec{{Name: "shell", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, calls, u := collect(t, ch)
	if streamed == nil || *streamed {
		t.Fatalf("stream = %v, want false", streamed)
	}
	if text != "bakıyorum" || len(calls) != 1 || calls[0].Name != "shell" || calls[0].ArgsJSON != `{"command":"hostname"}` {
		t.Fatalf("text=%q calls=%+v", text, calls)
	}
	if u.PromptTokens != 40 || u.CachedTokens != 12 {
		t.Fatalf("usage = %+v", u)
	}
}
