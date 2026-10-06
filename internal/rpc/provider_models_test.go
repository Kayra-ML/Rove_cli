package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/core"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

// Adding a provider fetches its models from {base}/models; the default
// agent moves onto a real one (not a guessed "gpt-4o"); refreshing picks
// up a changed list and moves agents off models that are gone.
func TestProviderModelsAreFetched(t *testing.T) {
	var mu sync.Mutex
	list := `{"data":[{"id":"qwen3-max"},{"id":"qwen3-coder"}]}`
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(list))
	}))
	defer srv.Close()

	app, err := core.Open(config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	s := New(app)
	ctx := context.Background()
	call := func(method string, params, out any) string {
		t.Helper()
		raw, _ := json.Marshal(params)
		resp := s.Dispatch(ctx, protocol.Request{Method: method, Token: app.Token, Params: raw})
		if !resp.OK {
			return resp.Error
		}
		if out != nil {
			_ = json.Unmarshal(resp.Result, out)
		}
		return ""
	}

	// the key first (as the settings form now does), then the provider
	if e := call(protocol.MethodSecretPut, map[string]any{"id": "qwen-key", "value": "sk-q"}, nil); e != "" {
		t.Fatal(e)
	}
	var saved core.ProviderResult
	if e := call(protocol.MethodProviderUpsert, map[string]any{"name": "qwen", "kind": "openai-compat", "baseUrl": srv.URL + "/v1", "secretId": "qwen-key", "models": []string{}}, &saved); e != "" {
		t.Fatal(e)
	}
	if saved.ModelsFetched != 2 || len(saved.Models) != 2 || saved.Models[0] != "qwen3-coder" || saved.ModelsError != "" {
		t.Fatalf("saved = %+v", saved)
	}
	mu.Lock()
	if auth != "Bearer sk-q" {
		t.Fatalf("auth = %q", auth)
	}
	mu.Unlock()
	agents, _ := app.Agents.List(ctx)
	if agents[0].Provider != "qwen" || agents[0].Model != "qwen3-coder" {
		t.Fatalf("default agent = %s/%s", agents[0].Provider, agents[0].Model)
	}
	var models []modelChoice
	call(protocol.MethodModelList, nil, &models)
	if len(models) < 2 {
		t.Fatalf("model.list = %+v", models)
	}

	// the provider's list changes: refresh adds and removes, and the agent
	// on a model that is gone moves to one that exists
	mu.Lock()
	list = `{"data":[{"id":"qwen3.8-max"},{"id":"qwen3-max"}]}`
	mu.Unlock()
	var refreshed core.ProviderResult
	if e := call(protocol.MethodProviderRefresh, map[string]any{"id": saved.ID}, &refreshed); e != "" {
		t.Fatal(e)
	}
	if len(refreshed.Models) != 2 || refreshed.Models[1] != "qwen3.8-max" {
		t.Fatalf("refreshed = %+v", refreshed.Models)
	}
	agents, _ = app.Agents.List(ctx)
	if agents[0].Model != "qwen3-max" {
		t.Fatalf("agent still on a gone model: %s", agents[0].Model)
	}

	// a provider that cannot be asked is saved with the reason, and no
	// made-up model
	var broken core.ProviderResult
	if e := call(protocol.MethodProviderUpsert, map[string]any{"name": "down", "kind": "openai-compat", "baseUrl": srv.URL + "/nope"}, &broken); e != "" {
		t.Fatal(e)
	}
	if broken.ModelsError == "" || len(broken.Models) != 0 {
		t.Fatalf("broken = %+v", broken)
	}
	if e := call(protocol.MethodProviderRefresh, map[string]any{"id": broken.ID}, nil); e == "" {
		t.Fatal("refreshing an unreachable provider should fail")
	}
	_ = types.ProviderOpenAICompat
}
