package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
)

// FetchModels asks a provider which models it serves: GET {base}/models, the
// endpoint OpenAI-compatible servers (OpenAI, Groq, OpenRouter, xAI, Ollama,
// LM Studio, vLLM…) and Anthropic share. It understands the usual shapes of
// the answer and returns the ids sorted.
func FetchModels(ctx context.Context, client *http.Client, kind types.ProviderKind, baseURL, key string) ([]string, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key != "" {
		if types.NormalizeProviderKind(kind) == types.ProviderAnthropic {
			req.Header.Set("x-api-key", key)
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	if types.NormalizeProviderKind(kind) == types.ProviderAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", base, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return nil, fmt.Errorf("%s/models: %s %s", base, resp.Status, msg)
	}
	return parseModels(body)
}

// parseModels reads {"data":[{"id":…}]} (OpenAI, Anthropic), {"models":
// [{"name"|"id"|"model":…}]} (Ollama and some gateways), or a bare list.
func parseModels(body []byte) ([]string, error) {
	type entry struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Model string `json:"model"`
	}
	var shaped struct {
		Data   []entry `json:"data"`
		Models []entry `json:"models"`
	}
	var entries []entry
	if err := json.Unmarshal(body, &shaped); err == nil {
		entries = append(shaped.Data, shaped.Models...)
	} else if err := json.Unmarshal(body, &entries); err != nil {
		var names []string
		if err := json.Unmarshal(body, &names); err != nil {
			return nil, fmt.Errorf("the model list is not JSON the app understands")
		}
		for _, n := range names {
			entries = append(entries, entry{ID: n})
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		id := e.ID
		if id == "" {
			id = e.Model
		}
		if id == "" {
			id = e.Name
		}
		id = strings.TrimPrefix(strings.TrimSpace(id), "models/")
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the provider lists no models")
	}
	sort.Strings(out)
	return out, nil
}
