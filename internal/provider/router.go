package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
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
		client = http.DefaultClient
	}
	key := ""
	if o.Keys != nil && o.SecretID != "" {
		var err error
		key, err = o.Keys(o.SecretID)
		if err != nil {
			return nil, err
		}
	}
	host := hostOf(o.BaseURL)
	body := map[string]any{
		"model":          req.Model,
		"messages":       toOpenAIMessages(req.Messages, host == "openrouter.ai"),
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
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
	base := strings.TrimRight(o.BaseURL, "/")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("provider: %s: %s", resp.Status, b)
	}
	ch := make(chan ChatDelta, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
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
		dec := json.NewDecoder(resp.Body)
		// SSE: read line-oriented
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, err := resp.Body.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				for {
					i := bytes.IndexByte(buf, '\n')
					if i < 0 {
						break
					}
					line := strings.TrimSpace(string(buf[:i]))
					buf = buf[i+1:]
					if line == "" || !strings.HasPrefix(line, "data:") {
						continue
					}
					payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
					if payload == "[DONE]" {
						ch <- ChatDelta{Done: true, ToolCalls: flushCalls()}
						return
					}
					var chunk sseChunk
					if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
						continue
					}
					if chunk.Usage != nil && (chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0) {
						u := &Usage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens}
						if d := chunk.Usage.PromptDetails; d != nil {
							u.CachedTokens = d.CachedTokens
						}
						if chunk.Usage.CacheRead > u.CachedTokens {
							u.CachedTokens = chunk.Usage.CacheRead
						}
						select {
						case <-ctx.Done():
							return
						case ch <- ChatDelta{Usage: u}:
						}
						if len(chunk.Choices) == 0 {
							continue
						}
					}
					if len(chunk.Choices) == 0 {
						continue
					}
					d := chunk.Choices[0].Delta
					ev := ChatDelta{Content: d.Content}
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
					if ev.Content == "" {
						continue
					}
					select {
					case <-ctx.Done():
						return
					case ch <- ev:
					}
				}
			}
			if err != nil {
				select {
				case ch <- ChatDelta{Done: true, ToolCalls: flushCalls()}:
				case <-ctx.Done():
				}
				return
			}
			_ = dec
		}
	}()
	return ch, nil
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

// hostOf is the base URL's host ("api.openai.com"), lower-cased.
func hostOf(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
