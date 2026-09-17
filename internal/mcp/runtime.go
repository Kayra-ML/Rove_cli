package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/aether-dev/aether/internal/tool"
	"github.com/aether-dev/aether/internal/types"
)

type ServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

type Runtime struct {
	mu      sync.Mutex
	servers map[string]*client
	tools   *tool.Runtime
}

func New(tr *tool.Runtime) *Runtime {
	return &Runtime{servers: map[string]*client{}, tools: tr}
}

type client struct {
	cfg    ServerConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	id     atomic.Int64
	mu     sync.Mutex
}

type rpcReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcErr         `json:"error"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (r *Runtime) Start(ctx context.Context, cfg ServerConfig) error {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
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
	cl := &client{cfg: cfg, cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}
	r.mu.Lock()
	r.servers[cfg.Name] = cl
	r.mu.Unlock()
	if err := cl.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "aether", "version": "0.1.0"},
	}); err != nil {
		return err
	}
	_ = cl.notify("notifications/initialized", map[string]any{})
	if r.tools != nil {
		if err := r.registerTools(ctx, cfg.Name, cl); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) registerTools(ctx context.Context, name string, cl *client) error {
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
		r.tools.Register(&mcpTool{prefix: name, name: t.Name, desc: t.Description, schema: t.InputSchema, cl: cl})
	}
	return nil
}

func (r *Runtime) Stop(name string) error {
	r.mu.Lock()
	cl, ok := r.servers[name]
	delete(r.servers, name)
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("mcp server not running: %s", name)
	}
	_ = cl.stdin.Close()
	return cl.cmd.Process.Kill()
}

func (r *Runtime) List() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.servers))
	for k := range r.servers {
		out = append(out, k)
	}
	return out
}

func (c *client) call(ctx context.Context, method string, params any) error {
	_, err := c.request(ctx, method, params)
	return err
}

func (c *client) notify(method string, params any) error {
	msg := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	b, _ := json.Marshal(msg)
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := fmt.Fprintf(c.stdin, "%s\n", b)
	return err
}

func (c *client) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.id.Add(1)
	req := rpcReq{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	b, _ := json.Marshal(req)
	c.mu.Lock()
	if _, err := fmt.Fprintf(c.stdin, "%s\n", b); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	line, err := c.stdout.ReadBytes('\n')
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	_ = ctx
	var resp rpcResp
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp: %s", resp.Error.Message)
	}
	return resp.Result, nil
}

type mcpTool struct {
	prefix string
	name   string
	desc   string
	schema json.RawMessage
	cl     *client
}

func (t *mcpTool) Name() string { return "mcp_" + t.prefix + "_" + t.name }
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
	raw, err := t.cl.request(ctx, "tools/call", map[string]any{"name": t.name, "arguments": json.RawMessage(args)})
	if err != nil {
		return tool.Result{Content: err.Error(), IsError: true}, err
	}
	return tool.Result{Content: string(raw)}, nil
}


