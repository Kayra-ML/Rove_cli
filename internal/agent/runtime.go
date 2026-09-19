package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aether-dev/aether/internal/checkpoint"
	"github.com/aether-dev/aether/internal/eventbus"
	"github.com/aether-dev/aether/internal/id"
	"github.com/aether-dev/aether/internal/memory"
	"github.com/aether-dev/aether/internal/provider"
	"github.com/aether-dev/aether/internal/session"
	"github.com/aether-dev/aether/internal/store"
	"github.com/aether-dev/aether/internal/tool"
	"github.com/aether-dev/aether/internal/types"
)

type Runtime struct {
	store    *store.Store
	bus      *eventbus.Bus
	sessions *session.Manager
	memory   *memory.System
	router   *provider.Router
	tools    *tool.Runtime
	checkpt  *checkpoint.Manager
	mu       sync.Mutex
	cancels  map[types.ID]context.CancelFunc
}

func New(s *store.Store, bus *eventbus.Bus, sess *session.Manager, mem *memory.System, r *provider.Router, t *tool.Runtime) *Runtime {
	return &Runtime{store: s, bus: bus, sessions: sess, memory: mem, router: r, tools: t, cancels: map[types.ID]context.CancelFunc{}}
}

// SetCheckpointManager wires in the checkpoint manager for auto-snapshotting
// before mutating tool calls (file writes, git commits, shell exec).
func (rt *Runtime) SetCheckpointManager(c *checkpoint.Manager) {
	rt.checkpt = c
}

func (rt *Runtime) Upsert(ctx context.Context, a types.Agent) (types.Agent, error) {
	now := time.Now().UTC()
	if a.ID == "" {
		a.ID = id.NewID()
		a.CreatedAt = now
	}
	if a.Status == "" {
		a.Status = types.AgentIdle
	}
	a.UpdatedAt = now
	return a, rt.store.UpsertAgent(ctx, a)
}

func (rt *Runtime) Get(ctx context.Context, id types.ID) (types.Agent, error) {
	return rt.store.GetAgent(ctx, id)
}

func (rt *Runtime) List(ctx context.Context) ([]types.Agent, error) {
	return rt.store.ListAgents(ctx)
}

type RunRequest struct {
	AgentID     types.ID
	SessionID   types.ID
	WorkspaceID types.ID
	Workspace   string
	CardID      types.ID
	UserMessage string
	SystemExtra string
	MaxTurns    int
}

type RunResult struct {
	Assistant   string
	Turns       int
	Done        bool
	ToolsCalled []string
	FilesEdited []string
}

func (rt *Runtime) Cancel(agentID types.ID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if c, ok := rt.cancels[agentID]; ok {
		c()
		delete(rt.cancels, agentID)
	}
}

func (rt *Runtime) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	rt.mu.Lock()
	if prev, ok := rt.cancels[req.AgentID]; ok {
		prev()
	}
	rt.cancels[req.AgentID] = cancel
	rt.mu.Unlock()
	defer func() {
		cancel()
		rt.mu.Lock()
		delete(rt.cancels, req.AgentID)
		rt.mu.Unlock()
	}()

	agent, err := rt.store.GetAgent(ctx, req.AgentID)
	if err != nil {
		return RunResult{}, err
	}
	agent.Status = types.AgentRunning
	agent.UpdatedAt = time.Now().UTC()
	_ = rt.store.UpsertAgent(ctx, agent)
	rt.emitStatus(agent)

	if req.UserMessage != "" && rt.sessions != nil && req.SessionID != "" {
		_, _ = rt.sessions.Append(ctx, types.Message{SessionID: req.SessionID, Role: types.RoleUser, Content: req.UserMessage})
	}

	completer, model, err := rt.router.Resolve(agent.Profile, agent.Provider, agent.Model)
	if err != nil {
		agent.Status = types.AgentFailed
		_ = rt.store.UpsertAgent(ctx, agent)
		rt.emitStatus(agent)
		return RunResult{}, err
	}

	maxTurns := req.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 12
	}

	var last string
	var allToolsCalled []string
	var allFilesEdited []string
	for turn := 0; turn < maxTurns; turn++ {
		msgs, err := rt.buildMessages(ctx, agent, req)
		if err != nil {
			return RunResult{}, err
		}
		chatReq := provider.ChatRequest{Model: model, Messages: msgs, Stream: true}
		if rt.tools != nil {
			chatReq.Tools = rt.tools.Specs()
		}
		ch, err := completer.Complete(ctx, chatReq)
		if err != nil {
			agent.Status = types.AgentFailed
			_ = rt.store.UpsertAgent(ctx, agent)
			rt.emitStatus(agent)
			return RunResult{}, err
		}
		var content strings.Builder
		var toolCalls []types.ToolCall
		for d := range ch {
			if d.Usage != nil && rt.router != nil {
				rt.router.Meter.Add(*d.Usage)
			}
			if d.Content != "" {
				content.WriteString(d.Content)
				if rt.bus != nil {
					rt.bus.Publish(types.Event{
						Type:  types.EventMessageDelta,
						Topic: "session." + string(req.SessionID),
						Payload: map[string]any{
							"sessionId": string(req.SessionID),
							"delta":     d.Content,
						},
					})
				}
			}
			if len(d.ToolCalls) > 0 {
				toolCalls = append(toolCalls, d.ToolCalls...)
			}
		}
		text := content.String()
		last = text
		if rt.sessions != nil && req.SessionID != "" {
			_, _ = rt.sessions.Append(ctx, types.Message{
				SessionID: req.SessionID,
				Role:      types.RoleAssistant,
				Content:   text,
				ToolCalls: toolCalls,
			})
		}
		if len(toolCalls) == 0 {
			agent.Status = types.AgentIdle
			_ = rt.store.UpsertAgent(ctx, agent)
			rt.emitStatus(agent)
			return RunResult{Assistant: last, Turns: turn + 1, Done: true, ToolsCalled: allToolsCalled, FilesEdited: allFilesEdited}, nil
		}
		for _, tc := range toolCalls {
			allToolsCalled = append(allToolsCalled, tc.Name)
			if rt.bus != nil {
				rt.bus.Publish(types.Event{
					Type:  types.EventToolStart,
					Topic: "session." + string(req.SessionID),
					Payload: map[string]any{
						"name":      tc.Name,
						"args":      json.RawMessage(tc.ArgsJSON),
						"sessionId": string(req.SessionID),
					},
				})
			}
			// Auto-checkpoint before any tool call that mutates the workspace.
			if req.Workspace != "" && rt.checkpt != nil && isMutatingTool(tc.Name) {
				_, _ = rt.checkpt.Take(req.Workspace, "auto:"+tc.Name)
			}
			res, callErr := rt.tools.Call(ctx, tc.Name, tool.Context{
				AgentID: req.AgentID, WorkspaceID: req.WorkspaceID, Workspace: req.Workspace,
				CardID: req.CardID, SessionID: req.SessionID,
			}, json.RawMessage(tc.ArgsJSON))
			out := res.Content
			if callErr != nil && out == "" {
				out = callErr.Error()
			}
			// Track file writes/edits from write/patch/create tool calls.
			if isFileEditTool(tc.Name) && tc.ArgsJSON != "" {
				if p := extractPathArg(tc.ArgsJSON); p != "" {
					allFilesEdited = append(allFilesEdited, p)
				}
			}
			if rt.sessions != nil && req.SessionID != "" {
				tr := &types.ToolResult{ToolCallID: tc.ID, Name: tc.Name, Content: out, IsError: res.IsError || callErr != nil}
				_, _ = rt.sessions.Append(ctx, types.Message{SessionID: req.SessionID, Role: types.RoleTool, Content: out, ToolResult: tr})
			}
			if rt.bus != nil {
				rt.bus.Publish(types.Event{
					Type:  types.EventToolResult,
					Topic: "session." + string(req.SessionID),
					Payload: map[string]any{
						"name":      tc.Name,
						"content":   out,
						"isError":   res.IsError || callErr != nil,
						"sessionId": string(req.SessionID),
					},
				})
			}
		}
	}
	agent.Status = types.AgentIdle
	_ = rt.store.UpsertAgent(ctx, agent)
	rt.emitStatus(agent)
	return RunResult{Assistant: last, Turns: maxTurns, Done: false, ToolsCalled: allToolsCalled, FilesEdited: allFilesEdited}, nil
}

func (rt *Runtime) buildMessages(ctx context.Context, agent types.Agent, req RunRequest) ([]provider.ChatMessage, error) {
	sys := agent.SystemPrompt
	if sys == "" {
		sys = "You are Rove Code, a local coding agent. Prefer tools over speculation. Stay inside the workspace. Do not invent files that are not there."
	}
	if req.Workspace != "" {
		sys += "\n\nWorkspace: " + req.Workspace
		if name := filepath.Base(req.Workspace); name != "" && name != "." && name != "/" {
			sys += "\nProject: " + name
		}
	}
	if req.SystemExtra != "" {
		sys += "\n\n" + req.SystemExtra
	}
	if rt.memory != nil {
		sys += rt.memory.PromptBlock(ctx, agent.ID, req.WorkspaceID, req.SessionID)
	}
	out := []provider.ChatMessage{{Role: "system", Content: sys}}
	if rt.sessions != nil && req.SessionID != "" {
		hist, err := rt.sessions.History(ctx, req.SessionID)
		if err != nil {
			return nil, err
		}
		for _, m := range hist {
			cm := provider.ChatMessage{Role: string(m.Role), Content: m.Content, ToolCalls: m.ToolCalls}
			if m.ToolResult != nil {
				cm.ToolCallID = m.ToolResult.ToolCallID
			}
			out = append(out, cm)
		}
	} else if req.UserMessage != "" {
		out = append(out, provider.ChatMessage{Role: "user", Content: req.UserMessage})
	}
	return out, nil
}

func (rt *Runtime) emitStatus(a types.Agent) {
	if rt.bus == nil {
		return
	}
	rt.bus.Publish(types.Event{
		Type:  types.EventAgentStatus,
		Topic: "agent." + string(a.ID),
		Payload: map[string]any{
			"id":     string(a.ID),
			"status": string(a.Status),
		},
	})
}

func (rt *Runtime) EnsureDefault(ctx context.Context) (types.Agent, error) {
	all, err := rt.store.ListAgents(ctx)
	if err != nil {
		return types.Agent{}, err
	}
	if len(all) > 0 {
		return all[0], nil
	}
	return rt.Upsert(ctx, types.Agent{
		Name:     "default",
		Profile:  "default",
		Model:    "fake",
		Provider: "fake",
		Status:   types.AgentIdle,
	})
}

func FormatRunError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("agent: %v", err)
}

// ── tool tracking helpers ─────────────────────────────────────────────────────

// isFileEditTool returns true for built-in tool names that modify files.
func isFileEditTool(name string) bool {
	switch name {
	case "write_file", "patch_file", "create_file", "edit_file", "replace_file", "append_file":
		return true
	}
	return false
}

// isMutatingTool returns true for tool names that mutate the workspace:
// file writes, git commits, and shell execution.
func isMutatingTool(name string) bool {
	if isFileEditTool(name) {
		return true
	}
	switch name {
	case "shell", "run_command", "exec", "bash",
		"git_commit", "git_commit_all", "git_push":
		return true
	}
	return false
}

// extractPathArg extracts a "path" field from a JSON args blob.
func extractPathArg(argsJSON string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return ""
	}
	if p, ok := m["path"].(string); ok {
		return p
	}
	return ""
}
