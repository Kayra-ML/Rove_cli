package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/Kayra-ML/rove/internal/types"
)

// AgentCLI hands a chat's turn to an agent system installed on this computer
// — Claude Code, Codex, Antigravity, Hermes — through its non-interactive
// mode, in the chat's folder, signed in with the user's own account. The
// system brings its own tools and does the work itself; what comes back is
// its text, streamed. A chat keeps one session with the system, so each turn
// sends only the new message.
type AgentCLI struct {
	NameVal string
	// System is the catalog id: claude-code, codex, antigravity, hermes.
	System string
	// Access is "edits" (change files, ask before anything else — which in a
	// non-interactive run means no) or "full" (no approval prompts).
	Access string
	// Sessions remembers each chat's session with the system.
	Sessions *AgentSessions
}

// Agent CLI systems the app knows how to drive.
const (
	SysClaudeCode  = "claude-code"
	SysCodex       = "codex"
	SysAntigravity = "antigravity"
	SysHermes      = "hermes"
)

// AgentBins maps a system to its program.
var AgentBins = map[string]string{
	SysClaudeCode:  "claude",
	SysCodex:       "codex",
	SysAntigravity: "agy",
	SysHermes:      "hermes",
}

// AgentURL is how an agent-cli provider is stored: agent://<system>?access=…
func AgentURL(system, access string) string {
	if access != "full" {
		access = "edits"
	}
	return "agent://" + system + "?access=" + access
}

// ParseAgentURL reads AgentURL back.
func ParseAgentURL(raw string) (system, access string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "agent" || u.Host == "" {
		return "", "", false
	}
	access = u.Query().Get("access")
	if access != "full" {
		access = "edits"
	}
	return u.Host, access, true
}

func (a *AgentCLI) Kind() types.ProviderKind { return types.ProviderAgentCLI }
func (a *AgentCLI) Name() string {
	if a.NameVal != "" {
		return a.NameVal
	}
	return a.System
}

// FindBin looks for a program where installers put it. An app opened from
// the Finder gets a bare PATH, so the usual places are searched too.
func FindBin(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".local", "bin"), filepath.Join(home, ".claude", "local"),
		filepath.Join(home, ".npm-global", "bin"), filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".cargo", "bin"), filepath.Join(home, "go", "bin"),
		"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin",
	}
	exe := name
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	for _, d := range dirs {
		p := filepath.Join(d, exe)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// richPath is PATH with the usual install places, for the agent's own
// child processes (node, git, python).
func richPath() string {
	home, _ := os.UserHomeDir()
	extra := []string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".bun", "bin"), filepath.Join(home, ".npm-global", "bin")}
	return strings.Join(append(extra, os.Getenv("PATH")), string(os.PathListSeparator))
}

// the first prompt of a chat new to the system carries the talk so far
const (
	carryMessages = 16
	carryChars    = 16000
)

// prompt is what the system is sent: the newest user message, and — when
// it has no session for this chat yet — the conversation before it.
// systemOf is the system message Rove sends: a plan's rules, a character,
// how to report.
func systemOf(req ChatRequest) string {
	var sys []string
	for _, m := range req.Messages {
		if m.Role == "system" && strings.TrimSpace(m.Content) != "" {
			sys = append(sys, strings.TrimSpace(m.Content))
		}
	}
	return strings.Join(sys, "\n\n")
}

// gate is what the chat's agent may do, read from the tools Rove offered
// it (its permissions already applied). A request offering no tools is a
// plain question or comes from a caller that sets none: no gate then.
type gate struct{ read, write, shell bool }

func gateOf(req ChatRequest) gate {
	if len(req.Tools) == 0 {
		return gate{true, true, true}
	}
	has := map[string]bool{}
	for _, t := range req.Tools {
		has[t.Name] = true
	}
	return gate{read: has["read_file"] || has["list_dir"], write: has["write_file"] || has["patch_file"], shell: has["shell"]}
}

func prompt(req ChatRequest, resuming bool) string {
	return promptWith(req, resuming, true)
}

// promptWith builds the text sent as the prompt. withSys puts Rove's system
// message in front of it, for a system with no other place for one.
func promptWith(req ChatRequest, resuming, withSys bool) string {
	last := -1
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			last = i
			break
		}
	}
	if last < 0 {
		return ""
	}
	ask := req.Messages[last].Content
	if resuming {
		return ask
	}
	// The system message carries what Rove asks of the agent — a plan's
	// rules and JSON shape, a character, how to report. A system with no
	// place for it gets it first, once per CLI session (a resumed session
	// already has it).
	sys := ""
	if withSys {
		sys = systemOf(req)
	}
	if sys != "" {
		ask = "Talimatlar (Rove):\n\n" + sys + "\n\n---\n\n" + ask
	}
	var prior []string
	for _, m := range req.Messages[:last] {
		if (m.Role != "user" && m.Role != "assistant") || strings.TrimSpace(m.Content) == "" {
			continue
		}
		prior = append(prior, fmt.Sprintf("[%s]: %s", m.Role, m.Content))
	}
	if len(prior) > carryMessages {
		prior = prior[len(prior)-carryMessages:]
	}
	carried := strings.Join(prior, "\n\n")
	if len(carried) > carryChars {
		carried = "…" + carried[len(carried)-carryChars:]
	}
	if carried == "" {
		return ask
	}
	if sys != "" {
		return "Talimatlar (Rove):\n\n" + sys + "\n\n---\n\nBu sohbetin şimdiye kadarki kısmı:\n\n" + carried + "\n\n---\nŞimdiki istek:\n\n" + req.Messages[last].Content
	}
	return "Bu sohbetin şimdiye kadarki kısmı:\n\n" + carried + "\n\n---\nŞimdiki istek:\n\n" + ask
}

func (a *AgentCLI) Complete(ctx context.Context, req ChatRequest) (<-chan ChatDelta, error) {
	bin := FindBin(AgentBins[a.System])
	if bin == "" {
		return nil, fmt.Errorf("%s is not installed on this computer", AgentBins[a.System])
	}
	key := req.CacheKey
	sid := ""
	if a.Sessions != nil && key != "" {
		sid = a.Sessions.Get(a.System, key)
	}
	// Claude Code takes Rove's system message as its own, on every call;
	// the others get it in front of the prompt
	text := promptWith(req, sid != "", a.System != SysClaudeCode)
	g := gateOf(req)
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("nothing to send")
	}
	dir := req.Workdir
	if fi, err := os.Stat(dir); dir == "" || err != nil || !fi.IsDir() || filepath.Dir(dir) == dir {
		dir, _ = os.UserHomeDir()
	}
	model := req.Model
	if model == "default" {
		model = ""
	}
	// a question is answered without changing anything, whatever access
	// the connection has for work
	full := a.Access == "full" && !req.Ask
	var args []string
	switch a.System {
	case SysClaudeCode:
		args = []string{"-p", text, "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
		if model != "" {
			args = append(args, "--model", model)
		}
		if sid != "" {
			args = append(args, "--resume", sid)
		}
		switch {
		case req.Ask, !g.write:
			// print mode refuses whatever needs a permission: reads only
			args = append(args, "--permission-mode", "default")
		case full && g.shell:
			args = append(args, "--dangerously-skip-permissions")
		default:
			args = append(args, "--permission-mode", "acceptEdits")
		}
		if sys := systemOf(req); sys != "" {
			args = append(args, "--append-system-prompt", sys)
		}
		// the agent's permissions, as Claude Code's own tool names
		var deny []string
		if !g.write {
			deny = append(deny, "Edit", "Write", "MultiEdit", "NotebookEdit")
		}
		if !g.shell {
			deny = append(deny, "Bash")
		}
		if !g.read {
			deny = append(deny, "Read", "Grep", "Glob", "LS")
		}
		if len(deny) > 0 && !req.Ask {
			args = append(args, "--disallowedTools", strings.Join(deny, ","))
		}
	case SysCodex:
		// `exec resume` takes fewer flags than `exec`: no -C (the process
		// already runs in the chat's folder) and no --sandbox (set through
		// -c instead), with the options before the session id
		args = []string{"exec"}
		if sid != "" {
			args = append(args, "resume")
		}
		args = append(args, "--json", "--skip-git-repo-check")
		if model != "" {
			args = append(args, "-m", model)
		}
		switch {
		// Codex edits through its shell: an agent that may not write gets
		// a read-only sandbox (it can still run read-only commands)
		case req.Ask, !g.write:
			args = append(args, "-c", `sandbox_mode="read-only"`)
		case full && g.shell:
			args = append(args, "--dangerously-bypass-approvals-and-sandbox")
		default:
			args = append(args, "-c", `sandbox_mode="workspace-write"`)
		}
		if sid != "" {
			args = append(args, sid)
		}
		args = append(args, text)
	case SysAntigravity:
		args = []string{"-p", text, "--output-format", "text"}
		if model != "" {
			args = append(args, "--model", model)
		}
		// no-ask mode only for an agent that may both write and run commands
		if full && g.write && g.shell {
			args = append(args, "--dangerously-skip-permissions")
		}
	case SysHermes:
		args = []string{"-z", text, "--in", dir}
		if model != "" {
			args = append(args, "-m", model)
		}
		if full && g.write && g.shell {
			args = append(args, "--yolo")
		}
	default:
		return nil, fmt.Errorf("unknown agent system %q", a.System)
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+richPath(), "NO_COLOR=1")
	cmd.Stdin = nil
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr tailBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	out := make(chan ChatDelta, 64)
	go func() {
		defer close(out)
		var usage *Usage
		var newSID string
		var failure string
		emit := func(s string) {
			if s != "" {
				out <- ChatDelta{Content: s}
			}
		}
		switch a.System {
		case SysClaudeCode:
			newSID, usage, failure = readClaude(stdout, emit)
		case SysCodex:
			newSID, usage, failure = readCodex(stdout, emit)
		default:
			readText(stdout, emit)
		}
		werr := cmd.Wait()
		if newSID != "" && a.Sessions != nil && key != "" {
			a.Sessions.Put(a.System, key, newSID)
		}
		if failure == "" && werr != nil && ctx.Err() == nil {
			failure = strings.TrimSpace(stderr.String())
			if failure == "" {
				failure = werr.Error()
			}
		}
		if failure != "" {
			// said in the chat: the run ends with the reason it failed
			out <- ChatDelta{Content: "\n\n⚠ " + AgentBins[a.System] + ": " + failure}
		}
		out <- ChatDelta{Done: true, Usage: usage}
	}()
	return out, nil
}

// readClaude follows Claude Code's stream-json: text as it is written, the
// session to resume next time, and what the turn cost.
func readClaude(r io.Reader, emit func(string)) (sid string, usage *Usage, failure string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	wrote := false
	for sc.Scan() {
		var ev struct {
			Type      string `json:"type"`
			SessionID string `json:"session_id"`
			Event     struct {
				Type         string `json:"type"`
				ContentBlock struct {
					Type string `json:"type"`
				} `json:"content_block"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			} `json:"event"`
			IsError bool   `json:"is_error"`
			Result  string `json:"result"`
			Usage   struct {
				Input  int `json:"input_tokens"`
				Output int `json:"output_tokens"`
				Cached int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.SessionID != "" {
			sid = ev.SessionID
		}
		switch ev.Type {
		case "stream_event":
			switch ev.Event.Type {
			case "content_block_start":
				// text after a tool call is a new paragraph
				if ev.Event.ContentBlock.Type == "text" && wrote {
					emit("\n\n")
				}
			case "content_block_delta":
				if ev.Event.Delta.Type == "text_delta" && ev.Event.Delta.Text != "" {
					emit(ev.Event.Delta.Text)
					wrote = true
				}
			}
		case "result":
			usage = &Usage{PromptTokens: ev.Usage.Input + ev.Usage.Cached, CompletionTokens: ev.Usage.Output, CachedTokens: ev.Usage.Cached}
			if ev.IsError {
				failure = ev.Result
			} else if !wrote && ev.Result != "" {
				emit(ev.Result)
			}
		}
	}
	return sid, usage, failure
}

// readCodex follows `codex exec --json`: the thread to resume, each agent
// message as it completes, and the turn's usage.
func readCodex(r io.Reader, emit func(string)) (sid string, usage *Usage, failure string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	wrote := false
	for sc.Scan() {
		var ev struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Message  string `json:"message"`
			Item     struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			Usage struct {
				Input  int `json:"input_tokens"`
				Cached int `json:"cached_input_tokens"`
				Output int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "thread.started":
			sid = ev.ThreadID
		case "item.completed":
			if (ev.Item.Type == "agent_message" || ev.Item.Type == "assistant_message") && ev.Item.Text != "" {
				if wrote {
					emit("\n\n")
				}
				emit(ev.Item.Text)
				wrote = true
			}
		case "turn.completed":
			usage = &Usage{PromptTokens: ev.Usage.Input, CompletionTokens: ev.Usage.Output, CachedTokens: ev.Usage.Cached}
		case "turn.failed", "error":
			failure = ev.Error.Message
			if failure == "" {
				failure = ev.Message
			}
		}
	}
	return sid, usage, failure
}

// readText passes plain output through as it comes.
func readText(r io.Reader, emit func(string)) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			emit(string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

// tailBuffer keeps the last few KB a program wrote to stderr: enough to say
// why it failed.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(t.buf)), "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, "\n")
}

// AgentSessions remembers, per system and chat, the session to resume, in a
// small file so a restart does not lose the thread.
type AgentSessions struct {
	mu   sync.Mutex
	path string
	m    map[string]string
}

func NewAgentSessions(path string) *AgentSessions {
	s := &AgentSessions{path: path, m: map[string]string{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s.m)
	}
	return s
}

func (s *AgentSessions) Get(system, chat string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[system+"|"+chat]
}

func (s *AgentSessions) Put(system, chat, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m[system+"|"+chat] == id {
		return
	}
	s.m[system+"|"+chat] = id
	if s.path == "" {
		return
	}
	if b, err := json.Marshal(s.m); err == nil {
		_ = os.WriteFile(s.path, b, 0o600)
	}
}

// Forget drops a chat's session, so the next turn starts a fresh one that
// carries the conversation (after the chat was truncated or compacted).
func (s *AgentSessions) Forget(chat string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for k := range s.m {
		if strings.HasSuffix(k, "|"+chat) {
			delete(s.m, k)
			changed = true
		}
	}
	if changed && s.path != "" {
		if b, err := json.Marshal(s.m); err == nil {
			_ = os.WriteFile(s.path, b, 0o600)
		}
	}
}
