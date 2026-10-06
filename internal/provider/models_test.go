package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kayra-ML/rove/internal/types"
)

func TestFetchModelsShapesAndAuth(t *testing.T) {
	var gotAuth, gotKey, gotVersion, gotPath string
	body := ""
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotKey, gotVersion = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	ctx := context.Background()

	body = `{"object":"list","data":[{"id":"qwen3-max"},{"id":"llama-4"},{"id":"qwen3-max"}]}`
	got, err := FetchModels(ctx, nil, types.ProviderOpenAICompat, srv.URL+"/v1/", "sk-1")
	if err != nil || strings.Join(got, ",") != "llama-4,qwen3-max" || gotPath != "/v1/models" || gotAuth != "Bearer sk-1" {
		t.Fatalf("openai = %v %v path=%s auth=%q", got, err, gotPath, gotAuth)
	}
	// Anthropic: its own key header and version
	body = `{"data":[{"id":"claude-sonnet-5","type":"model"}]}`
	if got, err := FetchModels(ctx, nil, types.ProviderAnthropic, srv.URL, "ak"); err != nil || got[0] != "claude-sonnet-5" || gotKey != "ak" || gotAuth != "" || gotVersion == "" {
		t.Fatalf("anthropic = %v %v key=%q auth=%q", got, err, gotKey, gotAuth)
	}
	// Ollama-style and "models/" prefixed ids; no key means no auth header
	body = `{"models":[{"name":"qwen2.5-coder:7b"},{"id":"models/gemini-2.5-pro"}]}`
	if got, err := FetchModels(ctx, nil, types.ProviderOpenAICompat, srv.URL, ""); err != nil || strings.Join(got, ",") != "gemini-2.5-pro,qwen2.5-coder:7b" || gotAuth != "" {
		t.Fatalf("ollama = %v %v auth=%q", got, err, gotAuth)
	}
	// errors say what happened
	status, body = http.StatusUnauthorized, `{"error":"bad key"}`
	if _, err := FetchModels(ctx, nil, types.ProviderOpenAICompat, srv.URL, "x"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("401 = %v", err)
	}
	status, body = http.StatusOK, `{"data":[]}`
	if _, err := FetchModels(ctx, nil, types.ProviderOpenAICompat, srv.URL, "x"); err == nil {
		t.Fatal("an empty list is not a model list")
	}
}
