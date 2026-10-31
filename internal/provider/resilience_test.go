package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Burak-ML/rove/internal/secrets"
)

func init() { retryDelay = time.Millisecond }

func drain(t *testing.T, ch <-chan ChatDelta) (string, error) {
	t.Helper()
	var b strings.Builder
	var err error
	for d := range ch {
		b.WriteString(d.Content)
		if d.Err != nil {
			err = d.Err
		}
	}
	return b.String(), err
}

func TestRetriesOverloadedServer(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	p := &OpenAICompat{BaseURL: srv.URL, HTTPClient: srv.Client()}
	ch, err := p.Complete(context.Background(), ChatRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	text, serr := drain(t, ch)
	if text != "ok" || serr != nil || hits.Load() != 3 {
		t.Fatalf("%q %v hits=%d", text, serr, hits.Load())
	}
}

func TestNoRetryOnClientError(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "bad model", http.StatusBadRequest)
	}))
	defer srv.Close()
	p := &OpenAICompat{BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, err := p.Complete(context.Background(), ChatRequest{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "bad model") || hits.Load() != 1 {
		t.Fatalf("%v hits=%d", err, hits.Load())
	}
}

func TestBrokenStreamIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"half\"}}]}\n\n")
		w.(http.Flusher).Flush()
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer srv.Close()
	p := &OpenAICompat{BaseURL: srv.URL, HTTPClient: srv.Client()}
	ch, err := p.Complete(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	text, serr := drain(t, ch)
	if text != "half" || serr == nil {
		t.Fatalf("%q %v", text, serr)
	}
}

func TestStalledStreamGivesUp(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	p := &OpenAICompat{BaseURL: srv.URL, HTTPClient: srv.Client(), IdleTimeout: 200 * time.Millisecond}
	ch, err := p.Complete(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, serr := drain(t, ch)
	if serr == nil || !strings.Contains(serr.Error(), "stalled") {
		t.Fatalf("%v", serr)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("watchdog too slow")
	}
}

func TestRetryAfterHeader(t *testing.T) {
	d, ok := retryAfter(&statusError{retryAfter: "7"})
	if !ok || d != 7*time.Second {
		t.Fatalf("%v %v", d, ok)
	}
	if d, _ := retryAfter(&statusError{retryAfter: "9999"}); d != time.Minute {
		t.Fatalf("not capped: %v", d)
	}
	if _, ok := retryAfter(&statusError{retryAfter: "Wed, 21 Oct 2015 07:28:00 GMT"}); ok {
		t.Fatal("dates are not read")
	}
}

func TestCachedTokensInEveryDialect(t *testing.T) {
	for name, usage := range map[string]string{
		"openai":    `{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":80}}`,
		"anthropic": `{"prompt_tokens":100,"completion_tokens":5,"cache_read_input_tokens":80}`,
		"deepseek":  `{"prompt_tokens":100,"completion_tokens":5,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":%s}\n\ndata: [DONE]\n\n", usage)
		}))
		p := &OpenAICompat{BaseURL: srv.URL, HTTPClient: srv.Client()}
		ch, err := p.Complete(context.Background(), ChatRequest{Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		cached := 0
		for d := range ch {
			if d.Usage != nil {
				cached = d.Usage.CachedTokens
			}
		}
		srv.Close()
		if cached != 80 {
			t.Errorf("%s: cached = %d", name, cached)
		}
	}
}

func TestLocalModelsNeedNoKey(t *testing.T) {
	var auth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Authorization"))
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	noKey := func(string) (string, error) { return "", secrets.ErrNotFound }
	// served on this machine: asked without a key
	p := &OpenAICompat{NameVal: "ollama", BaseURL: srv.URL, SecretID: "ollama-key", Keys: noKey, HTTPClient: srv.Client()}
	ch, err := p.Complete(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := drain(t, ch); text != "ok" || auth.Load() != "" {
		t.Fatalf("%q auth=%q", text, auth.Load())
	}
	// a remote service without a saved key says what to do
	r := &OpenAICompat{NameVal: "openai", BaseURL: "https://api.openai.com/v1", SecretID: "openai-key", Keys: noKey}
	if _, err := r.Complete(context.Background(), ChatRequest{Model: "m"}); err == nil || !strings.Contains(err.Error(), "Settings → Providers") {
		t.Fatalf("%v", err)
	}
}
