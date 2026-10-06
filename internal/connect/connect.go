// Package connect is the list of systems a user can connect their own
// account to — agent systems on this computer that sign in through the
// browser, model APIs that take a key, and local servers — with what it
// takes to install, sign in and use each. It finds what is already
// installed and signed in, and opens a terminal for the steps a person has
// to take themselves (an install, a browser sign-in).
package connect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Kayra-ML/rove/internal/provider"
	"gopkg.in/yaml.v3"
)

// Kind of connection.
const (
	KindAgent = "agent" // a program on this computer, signed in with the user's account
	KindAPI   = "api"   // a model API that takes a key
	KindLocal = "local" // a server on this computer
)

// System is one entry of the catalog. Commands here are fixed: the app
// only ever runs these, picked by ID, never a command sent by a client.
type System struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Blurb string `json:"blurb"`
	// agent systems
	Bin     string `json:"bin,omitempty"`
	Install string `json:"install,omitempty"`
	Login   string `json:"login,omitempty"`
	// api and local servers: an OpenAI-compatible endpoint
	BaseURL string `json:"baseUrl,omitempty"`
	NeedKey bool   `json:"needKey,omitempty"`
	// KeyURL is where the user gets a key (opened in the system browser);
	// Docs is where to read more.
	KeyURL string   `json:"keyUrl,omitempty"`
	Docs   string   `json:"docs,omitempty"`
	Models []string `json:"models,omitempty"`
}

// Catalog lists what can be connected.
var Catalog = []System{
	{ID: provider.SysClaudeCode, Name: "Claude Code", Kind: KindAgent, Bin: "claude",
		Blurb:   "Claude aboneliğin (Pro/Max) ya da Anthropic Console hesabınla; Claude Code kendi araçlarıyla sohbetin klasöründe çalışır.",
		Install: "curl -fsSL https://claude.ai/install.sh | bash", Login: "claude auth login",
		Docs: "https://docs.claude.com/en/docs/claude-code/overview", Models: []string{"default", "opus", "sonnet", "haiku"}},
	{ID: provider.SysCodex, Name: "Codex", Kind: KindAgent, Bin: "codex",
		Blurb:   "ChatGPT hesabın (Plus/Pro/Business) ya da OpenAI anahtarınla; Codex sohbetin klasöründe çalışır.",
		Install: "npm install -g @openai/codex", Login: "codex login",
		Docs: "https://developers.openai.com/codex/cli", Models: []string{"default"}},
	{ID: provider.SysAntigravity, Name: "Antigravity", Kind: KindAgent, Bin: "agy",
		Blurb:   "Google hesabınla Antigravity CLI; Gemini ve hesabındaki diğer modellerle sohbetin klasöründe çalışır.",
		Install: "curl -fsSL https://antigravity.google/cli/install.sh | bash", Login: "agy",
		Docs: "https://antigravity.google/docs/cli/best-practices/", Models: []string{"default"}},
	{ID: provider.SysHermes, Name: "Hermes Agent", Kind: KindAgent, Bin: "hermes",
		Blurb: "Nous Research'ün Hermes ajanı, kendi kurduğun sağlayıcı ve hesaplarla.",
		Login: "hermes model", Docs: "https://github.com/NousResearch/hermes-agent", Models: []string{"default"}},
	{ID: "openclaw", Name: "OpenClaw", Kind: KindLocal, BaseURL: "http://127.0.0.1:18789/v1", NeedKey: true,
		Blurb: "Bilgisayarındaki OpenClaw gateway'i (OpenAI uyumlu uç nokta açık olmalı: gateway.http.endpoints.chatCompletions.enabled); anahtar olarak gateway token'ı."},
	{ID: "ollama", Name: "Ollama", Kind: KindLocal, BaseURL: "http://localhost:11434/v1",
		Blurb: "Bilgisayarında çalışan yerel modeller; anahtar gerekmez.", Docs: "https://ollama.com/download"},
	{ID: "openai", Name: "OpenAI", Kind: KindAPI, BaseURL: "https://api.openai.com/v1", NeedKey: true,
		Blurb: "OpenAI API anahtarınla GPT modelleri.", KeyURL: "https://platform.openai.com/api-keys"},
	{ID: "anthropic", Name: "Anthropic API", Kind: KindAPI, BaseURL: "https://api.anthropic.com/v1/", NeedKey: true,
		Blurb: "Anthropic Console anahtarınla Claude modelleri.", KeyURL: "https://console.anthropic.com/settings/keys"},
	{ID: "gemini", Name: "Google Gemini", Kind: KindAPI, BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/", NeedKey: true,
		Blurb: "Google AI Studio anahtarınla Gemini modelleri.", KeyURL: "https://aistudio.google.com/apikey"},
	{ID: "openrouter", Name: "OpenRouter", Kind: KindAPI, BaseURL: "https://openrouter.ai/api/v1", NeedKey: true,
		Blurb: "Tek anahtarla birçok sağlayıcının modelleri.", KeyURL: "https://openrouter.ai/keys"},
	{ID: "deepseek", Name: "DeepSeek", Kind: KindAPI, BaseURL: "https://api.deepseek.com/v1", NeedKey: true,
		Blurb: "DeepSeek API anahtarınla.", KeyURL: "https://platform.deepseek.com/api_keys"},
	{ID: "xai", Name: "xAI Grok", Kind: KindAPI, BaseURL: "https://api.x.ai/v1", NeedKey: true,
		Blurb: "xAI anahtarınla Grok modelleri.", KeyURL: "https://console.x.ai"},
	{ID: "groq", Name: "Groq", Kind: KindAPI, BaseURL: "https://api.groq.com/openai/v1", NeedKey: true,
		Blurb: "Groq anahtarınla hızlı açık modeller.", KeyURL: "https://console.groq.com/keys"},
	{ID: "mistral", Name: "Mistral", Kind: KindAPI, BaseURL: "https://api.mistral.ai/v1", NeedKey: true,
		Blurb: "Mistral anahtarınla.", KeyURL: "https://console.mistral.ai/api-keys"},
}

// Find returns a catalog entry by ID.
func Find(id string) (System, bool) {
	for _, s := range Catalog {
		if s.ID == id {
			return s, true
		}
	}
	return System{}, false
}

// Status is a catalog entry with what was found on this computer.
type Status struct {
	System
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	// LoggedIn is nil when the system has no way to ask
	LoggedIn *bool  `json:"loggedIn,omitempty"`
	Account  string `json:"account,omitempty"`
}

// Detect looks at every agent system: installed, which version, signed in.
// Each check is short; they run side by side.
func Detect(ctx context.Context) []Status {
	out := make([]Status, len(Catalog))
	var wg sync.WaitGroup
	for i, s := range Catalog {
		out[i] = Status{System: s}
		if s.Kind != KindAgent {
			continue
		}
		wg.Add(1)
		go func(st *Status) {
			defer wg.Done()
			st.Path = provider.FindBin(st.Bin)
			if st.Path == "" {
				return
			}
			st.Installed = true
			st.Version = firstLine(run(ctx, st.Path, "--version"))
			st.LoggedIn, st.Account = signedIn(ctx, st.ID, st.Path)
		}(&out[i])
	}
	wg.Wait()
	return out
}

func signedIn(ctx context.Context, id, bin string) (*bool, string) {
	yes, no := true, false
	switch id {
	case provider.SysClaudeCode:
		var st struct {
			LoggedIn   bool   `json:"loggedIn"`
			AuthMethod string `json:"authMethod"`
			Email      string `json:"email"`
		}
		if json.Unmarshal([]byte(run(ctx, bin, "auth", "status", "--json")), &st) != nil {
			return nil, ""
		}
		if !st.LoggedIn {
			return &no, ""
		}
		return &yes, strings.TrimSpace(st.Email + " " + st.AuthMethod)
	case provider.SysCodex:
		s := run(ctx, bin, "login", "status")
		switch {
		case strings.Contains(strings.ToLower(s), "not logged in"):
			return &no, ""
		case strings.Contains(strings.ToLower(s), "logged in"):
			return &yes, firstLine(s)
		}
	}
	return nil, ""
}

func run(ctx context.Context, bin string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	b, _ := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	return strings.TrimSpace(string(b))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Step is what a person does in a terminal: install the system, or sign in
// (which opens their browser).
const (
	StepInstall = "install"
	StepLogin   = "login"
)

// OpenTerminal runs a catalog system's install or sign-in command in a new
// window of the system terminal, where the person can follow and answer it.
func OpenTerminal(id, step string) error {
	s, ok := Find(id)
	if !ok || s.Kind != KindAgent {
		return fmt.Errorf("unknown agent system %q", id)
	}
	cmd := s.Login
	if step == StepInstall {
		cmd = s.Install
	}
	if cmd == "" {
		return errors.New("this step is not available for " + s.Name)
	}
	if step == StepLogin {
		if bin := provider.FindBin(s.Bin); bin != "" {
			cmd = strings.Replace(cmd, s.Bin, shellQuote(bin), 1)
		}
	}
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`tell application "Terminal"
activate
do script %q
end tell`, cmd)
		return exec.Command("osascript", "-e", script).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "cmd", "/k", cmd).Start()
	default:
		for _, t := range []string{"x-terminal-emulator", "gnome-terminal", "konsole", "xterm"} {
			if p, err := exec.LookPath(t); err == nil {
				if t == "gnome-terminal" {
					return exec.Command(p, "--", "sh", "-c", cmd+"; exec $SHELL").Start()
				}
				return exec.Command(p, "-e", "sh", "-c", cmd+"; exec $SHELL").Start()
			}
		}
		return errors.New("no terminal found; run this yourself: " + cmd)
	}
}

func shellQuote(s string) string {
	if !strings.ContainsAny(s, " '\"\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// AccountModels lists the models a connected agent system offers the
// account it is signed in to, with "default" (the account's own pick)
// first. Each system keeps the list its account was given in a cache of its
// own; when that is not there yet, the names in the catalog stand in.
func AccountModels(id string) []string {
	out := []string{"default"}
	var names []string
	switch id {
	case provider.SysCodex:
		names = codexModels()
	case provider.SysClaudeCode:
		names = claudeModels()
	case provider.SysHermes:
		names = hermesModels()
	}
	if len(names) > 0 {
		return append(out, names...)
	}
	if s, ok := Find(id); ok {
		for _, m := range s.Models {
			if m != "default" {
				out = append(out, m)
			}
		}
	}
	return out
}

// codexModels reads ~/.codex/models_cache.json (or $CODEX_HOME's): the
// models the signed-in account may pick, in Codex's own order, without the
// ones it hides from its own picker.
func codexModels() []string {
	home := homeDir("CODEX_HOME", ".codex")
	b, err := os.ReadFile(filepath.Join(home, "models_cache.json"))
	if err != nil {
		return nil
	}
	var cache struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if json.Unmarshal(b, &cache) != nil {
		return nil
	}
	var out []string
	for _, m := range cache.Models {
		if m.Slug != "" && (m.Visibility == "" || m.Visibility == "list") {
			out = append(out, m.Slug)
		}
	}
	return out
}

func homeDir(env, sub string) string {
	if h := os.Getenv(env); h != "" {
		return h
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, sub)
}

// claudeModels reads the model catalog Claude Code keeps for the signed-in
// account (~/.claude/cache/model-catalog): the newest one for the CLI, its
// main models first, then the older ones it still offers.
func claudeModels() []string {
	dir := filepath.Join(homeDir("CLAUDE_CONFIG_DIR", ".claude"), "cache", "model-catalog")
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var newest string
	var newestAt time.Time
	var cat struct {
		Catalog struct {
			Surface string `json:"surface"`
			Config  struct {
				Models []struct {
					ID      string `json:"id"`
					Section string `json:"section"`
				} `json:"models"`
			} `json:"config"`
		} `json:"catalog"`
	}
	for _, f := range files {
		if !strings.HasSuffix(f, "-cc.json") {
			continue
		}
		if fi, err := os.Stat(f); err == nil && fi.ModTime().After(newestAt) {
			newest, newestAt = f, fi.ModTime()
		}
	}
	if newest == "" {
		return nil
	}
	b, err := os.ReadFile(newest)
	if err != nil || json.Unmarshal(b, &cat) != nil {
		return nil
	}
	var main, more []string
	for _, m := range cat.Catalog.Config.Models {
		switch {
		case m.ID == "":
		case m.Section == "main":
			main = append(main, m.ID)
		default:
			more = append(more, m.ID)
		}
	}
	return append(main, more...)
}

// hermesModels lists the models of the provider Hermes is set to use
// (~/.hermes/config.yaml), from the list Hermes keeps for it; its own
// default model comes first. A model of another provider would not run
// without switching Hermes over, so those are left out.
func hermesModels() []string {
	home := homeDir("HERMES_HOME", ".hermes")
	b, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		return nil
	}
	var cfg struct {
		Model struct {
			Default  string `yaml:"default"`
			Provider string `yaml:"provider"`
		} `yaml:"model"`
	}
	if yaml.Unmarshal(b, &cfg) != nil || cfg.Model.Provider == "" {
		return nil
	}
	var cache map[string]struct {
		Models []string `json:"models"`
	}
	var out []string
	if cb, err := os.ReadFile(filepath.Join(home, "provider_models_cache.json")); err == nil && json.Unmarshal(cb, &cache) == nil {
		out = cache[cfg.Model.Provider].Models
	}
	if d := cfg.Model.Default; d != "" {
		rest := slices.DeleteFunc(slices.Clone(out), func(m string) bool { return m == d })
		out = append([]string{d}, rest...)
	}
	return out
}
