package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Kayra-ML/rove/internal/types"
)

// to sends every request to the test server, whatever host the base URL
// names (so host-specific behaviour can be tested).
type to struct{ srv *httptest.Server }

func (t to) RoundTrip(r *http.Request) (*http.Response, error) {
	u, _ := url.Parse(t.srv.URL)
	r.URL.Scheme, r.URL.Host = u.Scheme, u.Host
	return http.DefaultTransport.RoundTrip(r)
}

func sseServer(t *testing.T, frames []string, got *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if got != nil {
			_ = json.Unmarshal(b, got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
}

func collect(t *testing.T, ch <-chan ChatDelta) (string, []types.ToolCall, Usage) {
	t.Helper()
	var text strings.Builder
	var calls []types.ToolCall
	var u Usage
	for d := range ch {
		text.WriteString(d.Content)
		calls = append(calls, d.ToolCalls...)
		if d.Usage != nil {
			u = *d.Usage
		}
	}
	return text.String(), calls, u
}

// Real servers stream a tool call in pieces: id and name first, then the
// arguments a few characters at a time. They arrive as one whole call.
func TestStreamedToolCallsArePutTogether(t *testing.T) {
	srv := sseServer(t, []string{
		`{"choices":[{"delta":{"content":"Okuyorum"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read_file","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"pa"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.go\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"list_dir","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":1200,"completion_tokens":30,"prompt_tokens_details":{"cached_tokens":1024}}}`,
	}, nil)
	defer srv.Close()
	o := &OpenAICompat{BaseURL: srv.URL}
	ch, err := o.Complete(context.Background(), ChatRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	text, calls, u := collect(t, ch)
	if text != "Okuyorum" {
		t.Fatalf("text = %q", text)
	}
	if len(calls) != 2 || calls[0].ID != "call_a" || calls[0].Name != "read_file" || calls[0].ArgsJSON != `{"path":"a.go"}` || calls[1].Name != "list_dir" {
		t.Fatalf("calls = %+v", calls)
	}
	if u.PromptTokens != 1200 || u.CachedTokens != 1024 {
		t.Fatalf("usage = %+v", u)
	}
}

// The next request carries the assistant's tool_calls (the tool results
// refer to them), images as parts, and host-specific extras only where the
// host takes them.
func TestRequestShape(t *testing.T) {
	var body map[string]any
	srv := sseServer(t, []string{`{"choices":[{"delta":{"content":"ok"}}]}`}, &body)
	defer srv.Close()
	msgs := []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "bak", Images: []string{"data:image/png;base64,AAAA"}},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "c1", Name: "read_file", ArgsJSON: `{"path":"a"}`}}},
		{Role: "tool", Content: "file", ToolCallID: "c1"},
	}
	send := func(base string, req ChatRequest) {
		t.Helper()
		body = nil
		o := &OpenAICompat{BaseURL: base, HTTPClient: &http.Client{Transport: to{srv}}}
		req.Model, req.Messages = "m", msgs
		ch, err := o.Complete(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		collect(t, ch)
	}

	send("https://api.openai.com/v1", ChatRequest{ReasoningEffort: "high", CacheKey: "S1"})
	m := body["messages"].([]any)
	asst := m[2].(map[string]any)
	calls, _ := asst["tool_calls"].([]any)
	if len(calls) != 1 || calls[0].(map[string]any)["id"] != "c1" || asst["content"] != nil {
		t.Fatalf("assistant = %+v", asst)
	}
	parts, _ := m[1].(map[string]any)["content"].([]any)
	if len(parts) != 2 || parts[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("user = %+v", m[1])
	}
	if body["reasoning_effort"] != "high" || body["prompt_cache_key"] != "S1" {
		t.Fatalf("openai extras = %v %v", body["reasoning_effort"], body["prompt_cache_key"])
	}

	send("https://openrouter.ai/api/v1", ChatRequest{ReasoningEffort: "low", CacheKey: "S1"})
	if r, _ := body["reasoning"].(map[string]any); r["effort"] != "low" || body["reasoning_effort"] != nil || body["prompt_cache_key"] != nil {
		t.Fatalf("openrouter extras = %v", body)
	}
	sys := body["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if sys["cache_control"] == nil {
		t.Fatalf("openrouter system prompt not marked cacheable: %v", sys)
	}

	// any other server: nothing it might not know
	send("https://api.groq.com/openai/v1", ChatRequest{CacheKey: "S1"})
	if body["prompt_cache_key"] != nil || body["reasoning_effort"] != nil || body["reasoning"] != nil {
		t.Fatalf("unknown host got extras: %v", body)
	}
	if _, ok := body["messages"].([]any)[0].(map[string]any)["content"].(string); !ok {
		t.Fatal("system prompt should stay a plain string")
	}
}
