package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Kayra-ML/rove/internal/tool"
	"github.com/Kayra-ML/rove/internal/types"
)

type ServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// Runtime runs the MCP servers the user added and offers their tools to the
// agents as "mcp_<slug>_<tool>".
type Runtime struct {
	mu      sync.Mutex
	servers map[string]*client // by slug
	errs    map[string]string  // by slug: why the last start failed
	tools   *tool.Runtime
}

func New(tr *tool.Runtime) *Runtime {
	return &Runtime{servers: map[string]*client{}, errs: map[string]string{}, tools: tr}
}

const (
	startTimeout = 30 * time.Second
	callTimeout  = 2 * time.Minute
	resultRunes  = 60000
)

type client struct {
	cfg     ServerConfig
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	id      atomic.Int64
	wmu     sync.Mutex
	pmu     sync.Mutex
	pending map[int64]chan rpcResp
	done    chan struct{} // closed when the server's output ends
}

type rpcReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcErr         `json:"error"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Start runs a server and registers its tools. A server of the same name
// running already is stopped first, so a changed config takes effect.
func (r *Runtime) Start(ctx context.Context, cfg ServerConfig) error {
	slug := types.MCPSlug(cfg.Name)
	_ = r.Stop(cfg.Name)
	err := r.start(ctx, slug, cfg)
	r.mu.Lock()
	if err != nil {
		r.errs[slug] = err.Error()
	} else {
		delete(r.errs, slug)
	}
	r.mu.Unlock()
	return err
}

func (r *Runtime) start(ctx context.Context, slug string, cfg ServerConfig) error {
	// the server outlives the call that starts it
	cmd := exec.Command(cfg.Command, cfg.Args...)
	// the user's environment (PATH above all: npx, uvx…) plus the server's own
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	cl := &client{cfg: cfg, cmd: cmd, stdin: stdin, pending: map[int64]chan rpcResp{}, done: make(chan struct{})}
	go cl.read(stdout)
	go func() { _ = cmd.Wait() }()
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	fail := func(err error) error {
		cl.close()
		return err
	}
	if err := cl.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "rove", "version": "0.2"},
	}); err != nil {
		return fail(err)
	}
	_ = cl.notify("notifications/initialized", map[string]any{})
	if r.tools != nil {
		if err := r.registerTools(ctx, slug, cl); err != nil {
			return fail(err)
		}
	}
	r.mu.Lock()
	r.servers[slug] = cl
	r.mu.Unlock()
	return nil
}

func (r *Runtime) registerTools(ctx context.Context, slug string, cl *client) error {
	raw, err := cl.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return err
	}
	var listed struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		return err
	}
	for _, t := range listed.Tools {
		r.tools.Register(&mcpTool{prefix: slug, name: t.Name, desc: t.Description, schema: t.InputSchema, cl: cl})
	}
	return nil
}

// Stop ends a server and takes its tools away.
func (r *Runtime) Stop(name string) error {
	slug := types.MCPSlug(name)
	r.mu.Lock()
	cl, ok := r.servers[slug]
	delete(r.servers, slug)
	delete(r.errs, slug)
	r.mu.Unlock()
	if r.tools != nil {
		r.tools.Unregister("mcp_" + slug + "_")
	}
	if !ok {
		return fmt.Errorf("mcp server not running: %s", name)
	}
	cl.close()
	return nil
}

// StopAll ends every server, when the app closes.
func (r *Runtime) StopAll() {
	r.mu.Lock()
	names := make([]string, 0, len(r.servers))
	for _, cl := range r.servers {
		names = append(names, cl.cfg.Name)
	}
	r.mu.Unlock()
	for _, n := range names {
		_ = r.Stop(n)
	}
}

// List names the servers running.
func (r *Runtime) List() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.servers))
	for _, cl := range r.servers {
		out = append(out, cl.cfg.Name)
	}
	return out
}

// Errors says, by server name slug, why a server failed to start.
func (r *Runtime) Errors() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.errs))
	for k, v := range r.errs {
		out[k] = v
	}
	return out
}

func (c *client) close() {
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

// read hands each response to the call waiting for it; log lines and
// requests from the server are skipped.
func (c *client) read(out io.Reader) {
	defer close(c.done)
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		var resp rpcResp
		if json.Unmarshal(sc.Bytes(), &resp) != nil || resp.ID == nil {
			continue
		}
		c.pmu.Lock()
		ch := c.pending[*resp.ID]
		delete(c.pending, *resp.ID)
		c.pmu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
}

func (c *client) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

func (c *client) call(ctx context.Context, method string, params any) error {
	_, err := c.request(ctx, method, params)
	return err
}

func (c *client) notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *client) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.id.Add(1)
	ch := make(chan rpcResp, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()
	forget := func() {
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
	}
	if err := c.write(rpcReq{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		forget()
		return nil, err
	}
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp: %s", resp.Error.Message)
		}
		return resp.Result, nil
	case <-c.done:
		forget()
		return nil, errors.New("mcp: the server exited")
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	}
}

type mcpTool struct {
	prefix string
	name   string
	desc   string
	schema json.RawMessage
	cl     *client
}

// Name keeps to the characters and length every provider accepts.
func (t *mcpTool) Name() string {
	var b strings.Builder
	for _, r := range t.name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	n := "mcp_" + t.prefix + "_" + b.String()
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

func (t *mcpTool) Description() string {
	if t.desc != "" {
		return t.desc
	}
	return "MCP tool " + t.name + " from " + t.prefix
}

func (t *mcpTool) Parameters() json.RawMessage {
	if len(t.schema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return t.schema
}

func (t *mcpTool) RequiredPermission() types.PermissionAction { return types.PermNetwork }

func (t *mcpTool) Call(ctx context.Context, _ tool.Context, args json.RawMessage) (tool.Result, error) {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	raw, err := t.cl.request(ctx, "tools/call", map[string]any{"name": t.name, "arguments": args})
	if err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, err
	}
	text, isErr := callText(raw)
	return tool.Result{Content: text, IsError: isErr}, nil
}

// callText reads a tools/call result: its text parts, or the raw result
// when it has none.
func callText(raw json.RawMessage) (string, bool) {
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	out := string(raw)
	if json.Unmarshal(raw, &res) == nil && len(res.Content) > 0 {
		var parts []string
		for _, c := range res.Content {
			if c.Type == "text" && c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
		if len(parts) > 0 {
			out = strings.Join(parts, "\n")
		}
	}
	if r := []rune(out); len(r) > resultRunes {
		out = string(r[:resultRunes]) + "\n…(cut)"
	}
	return out, res.IsError
}
