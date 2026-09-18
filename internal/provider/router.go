package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aether-dev/aether/internal/types"
)

var ErrNoProvider = errors.New("provider: none configured")

type ChatMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []types.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
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
}

type MeterSnapshot struct {
	PromptTokens     int64 `json:"promptTokens"`
	CompletionTokens int64 `json:"completionTokens"`
	TotalTokens      int64 `json:"totalTokens"`
	Calls            int64 `json:"calls"`
}

type Meter struct {
	mu               sync.Mutex
	promptTokens     int64
	completionTokens int64
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
	m.calls++
	snap := MeterSnapshot{
		PromptTokens: m.promptTokens, CompletionTokens: m.completionTokens,
		TotalTokens: m.promptTokens + m.completionTokens, Calls: m.calls,
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
		TotalTokens: m.promptTokens + m.completionTokens, Calls: m.calls,
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
	body := map[string]any{
		"model":          req.Model,
		"messages":       toOpenAIMessages(req.Messages),
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
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
						ch <- ChatDelta{Done: true}
						return
					}
					var chunk sseChunk
					if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
						continue
					}
					if chunk.Usage != nil && (chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0) {
						select {
						case <-ctx.Done():
							return
						case ch <- ChatDelta{Done: len(chunk.Choices) == 0, Usage: &Usage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens}}:
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
					for _, tc := range d.ToolCalls {
						ev.ToolCalls = append(ev.ToolCalls, types.ToolCall{ID: tc.ID, Name: tc.Function.Name, ArgsJSON: tc.Function.Arguments})
					}
					select {
					case <-ctx.Done():
						return
					case ch <- ev:
					}
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					select {
					case ch <- ChatDelta{Content: "", Done: true}:
					default:
					}
				} else {
					ch <- ChatDelta{Done: true}
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
	} `json:"usage"`
}

func toOpenAIMessages(msgs []ChatMessage) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		item := map[string]any{"role": m.Role, "content": m.Content}
		if m.ToolCallID != "" {
			item["tool_call_id"] = m.ToolCallID
		}
		out = append(out, item)
	}
	return out
}
