package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Burak-ML/rove/internal/secrets"
	"github.com/Burak-ML/rove/internal/types"
)

var ErrNoProvider = errors.New("provider: none configured")

type ChatMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []types.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	// Images are data: URLs sent with a user message to vision models.
	Images []string `json:"images,omitempty"`
}

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools    []ToolSpec    `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
	// ReasoningEffort ("low", "medium", "high"; empty = the model's own
	// default) asks reasoning models to think less or more.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	// CacheKey groups requests that share a prefix (a chat), so providers
	// that cache prompts can reuse it.
	CacheKey string `json:"cacheKey,omitempty"`
	// Workdir is the chat's folder: an agent system on this computer works
	// there.
	Workdir string `json:"workdir,omitempty"`
	// Ask marks a question to answer in text (a plan, a split, a summary),
	// not work: an agent system on this computer gets it read-only.
	Ask bool `json:"ask,omitempty"`
}

type ChatDelta struct {
	Content   string           `json:"content,omitempty"`
	ToolCalls []types.ToolCall `json:"toolCalls,omitempty"`
	Done      bool             `json:"done"`
	Usage     *Usage           `json:"usage,omitempty"`
	// Err, on the last delta, says the reply did not finish: the stream
	// broke or stalled. What came before it is incomplete.
	Err error `json:"-"`
	// Began marks the moment the model started answering, whatever it
	// started with (text, a tool call, reasoning). Tool calls are only sent
	// whole at the end, so without it a reply that calls a tool would seem
	// to start when it ended.
	Began bool `json:"-"`
}

type Usage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	// CachedTokens is the part of PromptTokens served from the provider's
	// prompt cache (cheaper).
	CachedTokens int `json:"cachedTokens,omitempty"`
}

type MeterSnapshot struct {
	PromptTokens     int64 `json:"promptTokens"`
	CompletionTokens int64 `json:"completionTokens"`
	TotalTokens      int64 `json:"totalTokens"`
	Calls            int64 `json:"calls"`
	CachedTokens     int64 `json:"cachedTokens"`
}

type Meter struct {
	mu               sync.Mutex
	promptTokens     int64
	completionTokens int64
	cachedTokens     int64
	calls            int64
	persist          func(MeterSnapshot)
}

func (m *Meter) SetPersist(fn func(MeterSnapshot)) {
	m.mu.Lock()
	m.persist = fn
	m.mu.Unlock()
}

func (m *Meter) Add(u Usage) {
	if u.PromptTokens == 0 && u.CompletionTokens == 0 {
		return
	}
	m.mu.Lock()
	m.promptTokens += int64(u.PromptTokens)
	m.completionTokens += int64(u.CompletionTokens)
	m.cachedTokens += int64(u.CachedTokens)
	m.calls++
	snap := MeterSnapshot{
		PromptTokens: m.promptTokens, CompletionTokens: m.completionTokens,
		TotalTokens: m.promptTokens + m.completionTokens, Calls: m.calls, CachedTokens: m.cachedTokens,
	}
	persist := m.persist
	m.mu.Unlock()
	if persist != nil {
		persist(snap)
	}
}

func (m *Meter) Snapshot() MeterSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return MeterSnapshot{
		PromptTokens: m.promptTokens, CompletionTokens: m.completionTokens,
		TotalTokens: m.promptTokens + m.completionTokens, Calls: m.calls, CachedTokens: m.cachedTokens,
	}
}

func (m *Meter) Load(s MeterSnapshot) {
	m.mu.Lock()
	m.promptTokens = s.PromptTokens
	m.completionTokens = s.CompletionTokens
	m.cachedTokens = s.CachedTokens
	m.calls = s.Calls
	m.mu.Unlock()
}

type Completer interface {
	Complete(ctx context.Context, req ChatRequest) (<-chan ChatDelta, error)
	Kind() types.ProviderKind
	Name() string
}

type KeyLookup func(secretID string) (string, error)

type Router struct {
	mu        sync.RWMutex
	providers map[string]Completer
	defaults  map[string]string // profile -> "provider/model"
	fallback  string
	Meter     Meter
}

func NewRouter() *Router {
	return &Router{providers: map[string]Completer{}, defaults: map[string]string{}}
}

func (r *Router) Register(name string, c Completer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = c
	if r.fallback == "" || (r.fallback == "fake" && name != "fake") {
		r.fallback = name
	}
}

func (r *Router) SetDefault(profile, provider string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaults[profile] = provider
	if provider != "" && provider != "fake" {
		r.fallback = provider
	}
}

func (r *Router) Get(name string) (Completer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name == "" {
		name = r.fallback
	}
	c, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoProvider, name)
	}
	return c, nil
}

func (r *Router) Resolve(profile, provider, model string) (Completer, string, error) {
	r.mu.RLock()
	if provider == "" {
		provider = r.defaults[profile]
	}
	if provider == "" {
		provider = r.fallback
	}
	r.mu.RUnlock()
	c, err := r.Get(provider)
	if err != nil {
		return nil, "", err
	}
	return c, model, nil
}

func (r *Router) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.providers))
	for k := range r.providers {
		out = append(out, k)
	}
	return out
}

// Fake is a deterministic completer used in tests and offline mode.
type Fake struct {
	NameVal   string
	Responses []string
	mu        sync.Mutex
	i         int
	Delay     time.Duration
}

func (f *Fake) Kind() types.ProviderKind { return types.ProviderFake }
func (f *Fake) Name() string {
	if f.NameVal == "" {
		return "fake"
	}
	return f.NameVal
}

func (f *Fake) Complete(ctx context.Context, req ChatRequest) (<-chan ChatDelta, error) {
	ch := make(chan ChatDelta, 8)
	go func() {
		defer close(ch)
		f.mu.Lock()
		idx := f.i
		if idx >= len(f.Responses) {
			idx = len(f.Responses) - 1
		}
		if idx < 0 {
			f.mu.Unlock()
			select {
			case ch <- ChatDelta{Content: "ok", Done: true}:
			case <-ctx.Done():
			}
			return
		}
		text := f.Responses[idx]
		f.i++
		f.mu.Unlock()
		if f.Delay > 0 {
			select {
			case <-time.After(f.Delay):
			case <-ctx.Done():
				return
			}
		}
		for _, part := range chunk(text, 24) {
			select {
			case <-ctx.Done():
				return
			case ch <- ChatDelta{Content: part}:
			}
		}
		ch <- ChatDelta{Done: true}
	}()
	return ch, nil
}

func chunk(s string, n int) []string {
	if n <= 0 || s == "" {
		return []string{s}
	}
	var out []string
	for len(s) > 0 {
		if len(s) < n {
			out = append(out, s)
			break
		}
		out = append(out, s[:n])
		s = s[n:]
	}
	return out
}

// OpenAICompat talks to any OpenAI-compatible chat completions endpoint.
type OpenAICompat struct {
	NameVal    string
	BaseURL    string
	SecretID   string
	Keys       KeyLookup
	HTTPClient *http.Client
	// IdleTimeout gives up a stream that sends nothing for this long
	// (0 = defaultStreamIdle).
	IdleTimeout time.Duration
}

func (o *OpenAICompat) Kind() types.ProviderKind { return types.ProviderOpenAICompat }
func (o *OpenAICompat) Name() string {
	if o.NameVal == "" {
		return "openai"
	}
	return o.NameVal
}

func (o *OpenAICompat) Complete(ctx context.Context, req ChatRequest) (<-chan ChatDelta, error) {
	client := o.HTTPClient
	if client == nil {
		client = defaultClient
	}
	host := hostOf(o.BaseURL)
	key := ""
	if o.Keys != nil && o.SecretID != "" {
		var err error
		key, err = o.Keys(o.SecretID)
		switch {
		case errors.Is(err, secrets.ErrNotFound) && isLocalHost(host):
			// a model served on this machine (Ollama, LM Studio, llama.cpp)
			// takes no key: ask it without one
			key = ""
		case errors.Is(err, secrets.ErrNotFound):
			return nil, fmt.Errorf("no API key saved for %s: add it in Settings → Providers", o.Name())
		case err != nil:
			return nil, err
		}
	}
	// A tool turn is asked for whole, not streamed. Some OpenAI-compatible
	// gateways (the one behind 89.47.113.13) answer a streamed request that
	// carries tools with the call written out as text — "<tool_call>…" —
	// and finish with "stop", so the command is never run. The same request
	// with stream off comes back as a real tool_calls entry. Plain replies,
	// which have no tools, still stream so the words appear as they come.
	stream := len(req.Tools) == 0
	body := map[string]any{
		"model":    req.Model,
		"messages": toOpenAIMessages(req.Messages, host == "openrouter.ai"),
		"stream":   stream,
	}
	if stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	// extras only where the endpoint is known to take them: an unknown
	// field can make a strict OpenAI-compatible server reject the request
	if req.ReasoningEffort != "" {
		if host == "openrouter.ai" {
			body["reasoning"] = map[string]any{"effort": req.ReasoningEffort}
		} else {
			body["reasoning_effort"] = req.ReasoningEffort
		}
	}
	if req.CacheKey != "" && host == "api.openai.com" {
		body["prompt_cache_key"] = req.CacheKey
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  json.RawMessage(t.Parameters),
				},
			})
		}
		body["tools"] = tools
	}
	raw, _ := json.Marshal(body)
	resp, err := o.post(ctx, client, strings.TrimRight(o.BaseURL, "/")+"/chat/completions", raw, key)
	if err != nil {
		return nil, err
	}
	idle := o.IdleTimeout
	if idle <= 0 {
		idle = defaultStreamIdle
	}
	if !stream {
		b, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			// the reply broke off: said the way a broken stream is, so the
			// run asks for the turn again
			ch := make(chan ChatDelta, 1)
			ch <- ChatDelta{Done: true, Err: fmt.Errorf("provider: reply broke off: %w", err)}
			close(ch)
			return ch, nil
		}
		return wholeReply(b)
	}
	ch := make(chan ChatDelta, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		send := func(d ChatDelta) bool {
			select {
			case <-ctx.Done():
				return false
			case ch <- d:
				return true
			}
		}
		// a stream that goes quiet is closed, so the run fails instead of
		// waiting forever on a server that will never answer
		var stalled atomic.Bool
		watchdog := time.AfterFunc(idle, func() {
			stalled.Store(true)
			_ = resp.Body.Close()
		})
		defer watchdog.Stop()
		// Tool calls arrive in pieces: the first piece of each (by index)
		// has its id and name, the rest only more of its arguments. They are
		// put together here and sent once, whole, when the stream ends.
		var calls []*types.ToolCall
		byIndex := map[int]*types.ToolCall{}
		flushCalls := func() []types.ToolCall {
			out := make([]types.ToolCall, 0, len(calls))
			for _, c := range calls {
				if c.Name != "" {
					if c.ID == "" {
						c.ID = fmt.Sprintf("call_%d", len(out))
					}
					out = append(out, *c)
				}
			}
			calls, byIndex = nil, map[int]*types.ToolCall{}
			return out
		}
		br := bufio.NewReaderSize(resp.Body, 32*1024)
		began := false
		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 {
				watchdog.Reset(idle)
			}
			line = bytes.TrimSpace(line)
			if payload, ok := bytes.CutPrefix(line, []byte("data:")); ok {
				payload = bytes.TrimSpace(payload)
				if string(payload) == "[DONE]" {
					send(ChatDelta{Done: true, ToolCalls: flushCalls()})
					return
				}
				var chunk sseChunk
				if json.Unmarshal(payload, &chunk) == nil {
					if !began && len(chunk.Choices) > 0 {
						began = true
						if !send(ChatDelta{Began: true}) {
							return
						}
					}
					if chunk.Usage != nil && (chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0) {
						u := &Usage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens}
						if d := chunk.Usage.PromptDetails; d != nil {
							u.CachedTokens = d.CachedTokens
						}
						u.CachedTokens = max(u.CachedTokens, chunk.Usage.CacheRead, chunk.Usage.CacheHit)
						if !send(ChatDelta{Usage: u}) {
							return
						}
					}
					if len(chunk.Choices) > 0 {
						d := chunk.Choices[0].Delta
						for n, tc := range d.ToolCalls {
							idx := n
							if tc.Index != nil {
								idx = *tc.Index
							}
							c, ok := byIndex[idx]
							// a piece with a new id starts a new call even
							// without an index (some servers send none)
							if !ok || (tc.ID != "" && c.ID != "" && tc.ID != c.ID) {
								c = &types.ToolCall{}
								byIndex[idx] = c
								calls = append(calls, c)
							}
							if tc.ID != "" {
								c.ID = tc.ID
							}
							if tc.Function.Name != "" {
								c.Name = tc.Function.Name
							}
							c.ArgsJSON += tc.Function.Arguments
						}
						if d.Content != "" && !send(ChatDelta{Content: d.Content}) {
							return
						}
					}
				}
			}
			if err != nil {
				switch {
				case ctx.Err() != nil:
				case err == io.EOF:
					// some servers end without [DONE]
					send(ChatDelta{Done: true, ToolCalls: flushCalls()})
				case stalled.Load():
					// half-sent tool calls are dropped: their arguments are cut
					send(ChatDelta{Done: true, Err: fmt.Errorf("provider: stream stalled: nothing received for %s", idle)})
				default:
					send(ChatDelta{Done: true, Err: fmt.Errorf("provider: stream broke off: %w", err)})
				}
				return
			}
		}
	}()
	return ch, nil
}

// wholeReply reads one non-streamed chat completion — the shape a tool turn
// comes back in — and hands it over as a single delta, the way a finished
// stream would.
func wholeReply(b []byte) (<-chan ChatDelta, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			PromptDetails    *struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CacheRead int `json:"cache_read_input_tokens"`
			CacheHit  int `json:"prompt_cache_hit_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("provider: bad reply: %w", err)
	}
	d := ChatDelta{Done: true}
	if len(resp.Choices) > 0 {
		m := resp.Choices[0].Message
		d.Content = m.Content
		for i, tc := range m.ToolCalls {
			if tc.Function.Name == "" {
				continue
			}
			id := tc.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			d.ToolCalls = append(d.ToolCalls, types.ToolCall{ID: id, Name: tc.Function.Name, ArgsJSON: tc.Function.Arguments})
		}
	}
	if u := resp.Usage; u != nil && (u.PromptTokens > 0 || u.CompletionTokens > 0) {
		d.Usage = &Usage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens}
		if p := u.PromptDetails; p != nil {
			d.Usage.CachedTokens = p.CachedTokens
		}
		d.Usage.CachedTokens = max(d.Usage.CachedTokens, u.CacheRead, u.CacheHit)
	}
	ch := make(chan ChatDelta, 1)
	ch <- d
	close(ch)
	return ch, nil
}

// defaultStreamIdle is how long a stream may stay silent before it is
// given up. Reasoning models can think a while before their first word, so
// it is generous; it is there for servers that hang, not for slow ones.
const defaultStreamIdle = 3 * time.Minute

// defaultClient waits at most this long for a server to start answering. A
// tool turn is asked for whole (see Complete), and a whole reply's headers
// come only once it is written, so this is long.
var defaultClient = &http.Client{Transport: func() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = 10 * time.Minute
	return t
}()}

// retryDelay is the first wait before a retry; it doubles each time.
var retryDelay = time.Second

const maxAttempts = 4

// post sends the request, trying again on rate limits, overloaded or
// failing servers and dropped connections, with growing waits (or the wait
// the server asks for). Other errors return at once.
func (o *OpenAICompat) post(ctx context.Context, client *http.Client, endpoint string, raw []byte, key string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			wait := retryDelay << (attempt - 1)
			if ra, ok := retryAfter(lastErr); ok {
				wait = ra
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if key != "" {
			httpReq.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}
		if resp.StatusCode < 300 {
			return resp, nil
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		lastErr = &statusError{status: resp.Status, code: resp.StatusCode, body: b, retryAfter: resp.Header.Get("Retry-After")}
		if !retryable(resp.StatusCode) {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

type statusError struct {
	status     string
	code       int
	body       []byte
	retryAfter string
}

func (e *statusError) Error() string { return fmt.Sprintf("provider: %s: %s", e.status, e.body) }

func retryable(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		return true
	}
	return false
}

// retryAfter reads a Retry-After header given in seconds, capped at a minute.
func retryAfter(err error) (time.Duration, bool) {
	var se *statusError
	if !errors.As(err, &se) || se.retryAfter == "" {
		return 0, false
	}
	n, perr := strconv.Atoi(strings.TrimSpace(se.retryAfter))
	if perr != nil || n < 0 {
		return 0, false
	}
	return min(time.Duration(n)*time.Second, time.Minute), true
}

type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    *int   `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		PromptDetails    *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CacheRead int `json:"cache_read_input_tokens"`
		// DeepSeek's name for the part served from its prompt cache
		CacheHit int `json:"prompt_cache_hit_tokens"`
	} `json:"usage"`
}

// toOpenAIMessages writes the chat for an OpenAI-compatible endpoint. An
// assistant turn keeps its tool_calls (the tool results that follow refer
// to them); a user turn with images becomes text + image parts. markCache
// marks the system prompt as cacheable (OpenRouter passes it to Anthropic
// and Gemini models, which cache only what is marked).
func toOpenAIMessages(msgs []ChatMessage, markCache bool) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		item := map[string]any{"role": m.Role, "content": m.Content}
		switch {
		case m.Role == "system" && markCache && m.Content != "":
			item["content"] = []map[string]any{{"type": "text", "text": m.Content, "cache_control": map[string]any{"type": "ephemeral"}}}
		case len(m.Images) > 0:
			parts := []map[string]any{{"type": "text", "text": m.Content}}
			for _, img := range m.Images {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": img}})
			}
			item["content"] = parts
		}
		if len(m.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				args := tc.ArgsJSON
				if args == "" {
					args = "{}"
				}
				calls = append(calls, map[string]any{"id": tc.ID, "type": "function", "function": map[string]any{"name": tc.Name, "arguments": args}})
			}
			item["tool_calls"] = calls
			if m.Content == "" {
				item["content"] = nil
			}
		}
		if m.ToolCallID != "" {
			item["tool_call_id"] = m.ToolCallID
		}
		out = append(out, item)
	}
	return out
}

// isLocalHost: an endpoint on this machine or its local network name.
func isLocalHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0", "host.docker.internal":
		return true
	}
	return strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasPrefix(host, "127.")
}

// hostOf is the base URL's host ("api.openai.com"), lower-cased.
func hostOf(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
