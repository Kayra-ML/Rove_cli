package provider

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAgentURL(t *testing.T) {
	sys, access, ok := ParseAgentURL(AgentURL(SysCodex, "full"))
	if !ok || sys != SysCodex || access != "full" {
		t.Fatalf("parse = %q %q %v", sys, access, ok)
	}
	if _, access, _ := ParseAgentURL(AgentURL(SysClaudeCode, "anything")); access != "edits" {
		t.Fatalf("access defaults to edits, got %q", access)
	}
	if _, _, ok := ParseAgentURL("https://api.openai.com/v1"); ok {
		t.Fatal("an API URL is not an agent")
	}
}

// A chat new to the system carries Rove's instructions and the talk so far;
// a resumed one sends only the new message.
func TestAgentPrompt(t *testing.T) {
	req := ChatRequest{Messages: []ChatMessage{
		{Role: "system", Content: "rules"},
		{Role: "user", Content: "ilk"},
		{Role: "assistant", Content: "cevap"},
		{Role: "tool", Content: "çıktı"},
		{Role: "user", Content: "ikinci"},
	}}
	if got := prompt(req, true); got != "ikinci" {
		t.Fatalf("resumed prompt = %q", got)
	}
	got := prompt(req, false)
	for _, want := range []string{"Talimatlar (Rove):\n\nrules", "[user]: ilk", "[assistant]: cevap", "Şimdiki istek:\n\nikinci"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fresh prompt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "çıktı") || strings.Index(got, "rules") > strings.Index(got, "ilk") {
		t.Fatalf("tool output carried, or the instructions are not first:\n%s", got)
	}
	// a one-off question (a planner's) is its instructions and the ask
	one := prompt(ChatRequest{Messages: []ChatMessage{{Role: "system", Content: "reply JSON"}, {Role: "user", Content: "plan"}}}, false)
	if one != "Talimatlar (Rove):\n\nreply JSON\n\n---\n\nplan" {
		t.Fatalf("question prompt = %q", one)
	}
}

func TestReadCodex(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th_1"}`,
		`{"type":"item.completed","item":{"type":"reasoning","text":"thinking"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"Bitti."}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"İkinci."}}`,
		`{"type":"turn.completed","usage":{"input_tokens":120,"cached_input_tokens":20,"output_tokens":9}}`,
	}, "\n")
	var b strings.Builder
	sid, u, fail := readCodex(strings.NewReader(in), func(s string) { b.WriteString(s) })
	if sid != "th_1" || fail != "" || b.String() != "Bitti.\n\nİkinci." || u == nil || u.PromptTokens != 120 || u.CompletionTokens != 9 {
		t.Fatalf("sid=%q fail=%q text=%q usage=%+v", sid, fail, b.String(), u)
	}
}

// The real path: a stand-in `claude` on PATH that answers in stream-json.
// The chat's session is kept and the next turn resumes it.
func TestAgentCLIClaudeStream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := `#!/bin/sh
printf '%s\n' "$@" > "` + log + `"
echo '{"type":"system","subtype":"init","session_id":"s-42"}'
echo '{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"text"}}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Mer"}}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"haba"}}}'
echo '{"type":"result","is_error":false,"result":"Merhaba","session_id":"s-42","usage":{"input_tokens":3,"output_tokens":2,"cache_read_input_tokens":10}}'
`
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	work := t.TempDir()
	a := &AgentCLI{System: SysClaudeCode, Access: "edits", Sessions: NewAgentSessions(filepath.Join(dir, "sessions.json"))}
	run := func(text string) (string, *Usage) {
		ch, err := a.Complete(context.Background(), ChatRequest{Model: "sonnet", CacheKey: "chat1", Workdir: work, Messages: []ChatMessage{{Role: "user", Content: text}}})
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		var u *Usage
		for d := range ch {
			b.WriteString(d.Content)
			if d.Usage != nil {
				u = d.Usage
			}
		}
		return b.String(), u
	}
	text, u := run("selam")
	if text != "Merhaba" || u == nil || u.PromptTokens != 13 || u.CompletionTokens != 2 {
		t.Fatalf("text=%q usage=%+v", text, u)
	}
	args, _ := os.ReadFile(log)
	for _, want := range []string{"-p\nselam\n", "--model\nsonnet", "--permission-mode\nacceptEdits"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("args lack %q:\n%s", want, args)
		}
	}
	if a.Sessions.Get(SysClaudeCode, "chat1") != "s-42" {
		t.Fatal("the chat's session is kept")
	}
	run("devam")
	args, _ = os.ReadFile(log)
	if !strings.Contains(string(args), "--resume\ns-42") {
		t.Fatalf("second turn resumes:\n%s", args)
	}
	// the store survives a restart
	if NewAgentSessions(filepath.Join(dir, "sessions.json")).Get(SysClaudeCode, "chat1") != "s-42" {
		t.Fatal("sessions are saved to disk")
	}
	a.Sessions.Forget("chat1")
	if a.Sessions.Get(SysClaudeCode, "chat1") != "" {
		t.Fatal("forget drops the chat's session")
	}
}

// Codex's resume takes fewer flags than a fresh exec: no -C and no
// --sandbox, options before the session id. A real run refused -C there and
// every later turn of the chat failed.
func TestAgentCLICodexResumeArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := `#!/bin/sh
printf '%s\n' "$@" > "` + log + `"
echo '{"type":"thread.started","thread_id":"th-9"}'
echo '{"type":"item.completed","item":{"type":"agent_message","text":"tamam"}}'
echo '{"type":"turn.completed","usage":{"input_tokens":5,"output_tokens":1}}'
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := &AgentCLI{System: SysCodex, Access: "edits", Sessions: NewAgentSessions("")}
	run := func(text string) string {
		ch, err := a.Complete(context.Background(), ChatRequest{Model: "gpt-6-sol", CacheKey: "c", Workdir: t.TempDir(), Messages: []ChatMessage{{Role: "user", Content: text}}})
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for d := range ch {
			b.WriteString(d.Content)
		}
		return b.String()
	}
	if got := run("ilk"); got != "tamam" {
		t.Fatalf("reply = %q", got)
	}
	args, _ := os.ReadFile(log)
	if !strings.HasPrefix(string(args), "exec\n--json\n--skip-git-repo-check\n-m\ngpt-6-sol\n-c\nsandbox_mode=\"workspace-write\"\n") || strings.Contains(string(args), "--sandbox") {
		t.Fatalf("fresh args:\n%s", args)
	}
	run("ikinci")
	args, _ = os.ReadFile(log)
	want := "exec\nresume\n--json\n--skip-git-repo-check\n-m\ngpt-6-sol\n-c\nsandbox_mode=\"workspace-write\"\nth-9\nikinci\n"
	if string(args) != want {
		t.Fatalf("resume args:\n%s\nwant:\n%s", args, want)
	}
	// a question runs read-only, even on a connection with full access
	full := &AgentCLI{System: SysCodex, Access: "full"}
	ch, err := full.Complete(context.Background(), ChatRequest{Ask: true, Messages: []ChatMessage{{Role: "user", Content: "plan?"}}})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	args, _ = os.ReadFile(log)
	if !strings.Contains(string(args), "sandbox_mode=\"read-only\"") || strings.Contains(string(args), "dangerously") {
		t.Fatalf("question args:\n%s", args)
	}
}

// The agent's character reaches Claude Code as its system prompt (not mixed
// into the message), and what the agent may not do becomes Claude Code's and
// Codex's own limits.
func TestAgentCLIPermissionsAndSystem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	for _, name := range []string{"claude", "codex"} {
		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + log + "\"\n"
		if name == "claude" {
			script += `echo '{"type":"result","is_error":false,"result":"ok","session_id":"s"}'` + "\n"
		} else {
			script += `echo '{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}'` + "\n"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	readOnly := []ToolSpec{{Name: "read_file"}, {Name: "list_dir"}}
	req := ChatRequest{Workdir: t.TempDir(), Tools: readOnly, Messages: []ChatMessage{{Role: "system", Content: "You are a security engineer."}, {Role: "user", Content: "review"}}}
	run := func(a *AgentCLI) string {
		ch, err := a.Complete(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		for range ch {
		}
		b, _ := os.ReadFile(log)
		return string(b)
	}
	got := run(&AgentCLI{System: SysClaudeCode, Access: "full"})
	if !strings.Contains(got, "--append-system-prompt\nYou are a security engineer.\n") {
		t.Fatalf("no system prompt:\n%s", got)
	}
	if strings.Contains(got, "Talimatlar") || !strings.Contains(got, "-p\nreview\n") {
		t.Fatalf("the character was mixed into the message:\n%s", got)
	}
	if !strings.Contains(got, "--disallowedTools\nEdit,Write,MultiEdit,NotebookEdit,Bash\n") || strings.Contains(got, "dangerously") {
		t.Fatalf("a read-only agent was let write or run:\n%s", got)
	}
	got = run(&AgentCLI{System: SysCodex, Access: "full"})
	if !strings.Contains(got, `sandbox_mode="read-only"`) || strings.Contains(got, "dangerously") {
		t.Fatalf("codex for a read-only agent:\n%s", got)
	}
	// with every permission, nothing is held back
	req.Tools = append(readOnly, ToolSpec{Name: "write_file"}, ToolSpec{Name: "shell"})
	if got = run(&AgentCLI{System: SysClaudeCode, Access: "full"}); strings.Contains(got, "disallowedTools") || !strings.Contains(got, "--dangerously-skip-permissions") {
		t.Fatalf("full agent:\n%s", got)
	}
}
