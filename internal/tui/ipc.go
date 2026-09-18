package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aether-dev/aether/internal/config"
	"github.com/aether-dev/aether/internal/types"
	"github.com/aether-dev/aether/pkg/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// IPCClient wraps the unix socket connection to the Aether daemon.
type IPCClient struct {
	ipcPath  string
	httpBase string
	token    string
	reqID    atomic.Int64
	// tcpAddr is non-empty when connected via SSH tunnel (overrides ipcPath)
	tcpAddr string
}

// --- Tea messages delivered by the IPC layer ---

type SessionListMsg struct {
	Sessions         []types.Session
	CreatedSessionID string
	Err              error
}

type SessionHistoryMsg struct {
	SessionID string
	Messages  []types.Message
	Err       error
}

type AgentListMsg struct {
	Agents []types.Agent
	Err    error
}

type UsageMsg struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	Calls            int64
	Err              error
}

type SetupMsg struct {
	Name  string
	Model string
	Err   error
}

type SendMsg struct {
	SessionID string
	Err       error
}

type EventMsg struct {
	Frame protocol.EventFrame
}

type ConnectedMsg struct{}
type DisconnectedMsg struct{ Err error }

type ProfilesMsg struct{ Profiles []types.AgentProfile }
type LinkedSessionsMsg struct {
	Links    []types.SessionLink
	Sessions []types.Session
}
type SessionLinkedMsg struct{ Link types.SessionLink }

// NewIPCClient creates a client using config defaults.
func NewIPCClient() (*IPCClient, error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, err
	}

	tok := os.Getenv("AETHER_TOKEN")
	if tok == "" {
		b, readErr := os.ReadFile(config.TokenPath(cfg.DataDir))
		if readErr == nil {
			tok = strings.TrimSpace(string(b))
		}
	}

	// Also try auth.token path directly
	if tok == "" {
		home, _ := os.UserHomeDir()
		tokenPaths := []string{
			filepath.Join(home, ".local", "share", "aether", "auth.token"),
			filepath.Join(home, ".local", "share", "aether", "token"),
			config.TokenPath(cfg.DataDir),
		}
		for _, tp := range tokenPaths {
			b, readErr := os.ReadFile(tp)
			if readErr == nil && len(b) > 0 {
				tok = strings.TrimSpace(string(b))
				break
			}
		}
	}

	return &IPCClient{
		ipcPath:  cfg.ListenIPC,
		httpBase: "http://" + cfg.ListenHTTP,
		token:    tok,
	}, nil
}

// NewIPCClientTCP creates an IPC client that connects via TCP (for SSH tunnels).
// addr is "localhost:port", token is the remote daemon token.
func NewIPCClientTCP(addr string, token string) *IPCClient {
	return &IPCClient{
		tcpAddr:  addr,
		httpBase: "http://" + addr,
		token:    token,
	}
}

func (c *IPCClient) nextID() string {
	return fmt.Sprintf("tui-%d", c.reqID.Add(1))
}

// Call sends a JSON-RPC request over the unix socket (or TCP tunnel) and returns the raw result.
func (c *IPCClient) Call(method string, params any) (json.RawMessage, error) {
	raw, _ := json.Marshal(params)
	req := protocol.Request{
		ID:     c.nextID(),
		Method: method,
		Params: raw,
		Token:  c.token,
	}

	// If using SSH tunnel, connect via TCP
	if c.tcpAddr != "" {
		conn, err := net.DialTimeout("tcp", c.tcpAddr, 5*time.Second)
		if err != nil {
			return nil, fmt.Errorf("ipc tcp dial %s: %w", c.tcpAddr, err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
		if err := json.NewEncoder(conn).Encode(req); err != nil {
			return nil, fmt.Errorf("ipc encode: %w", err)
		}
		var resp protocol.Response
		if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
			return nil, fmt.Errorf("ipc decode: %w", err)
		}
		if !resp.OK {
			return nil, fmt.Errorf("rpc error: %s", resp.Error)
		}
		return resp.Result, nil
	}

	// Try IPC first
	conn, err := net.DialTimeout("unix", c.ipcPath, 3*time.Second)
	if err != nil {
		// Fall back to HTTP
		return c.callHTTP(req)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("ipc encode: %w", err)
	}
	var resp protocol.Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("ipc decode: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("rpc error: %s", resp.Error)
	}
	return resp.Result, nil
}

func (c *IPCClient) callHTTP(req protocol.Request) (json.RawMessage, error) {
	hc := &http.Client{Timeout: 30 * time.Second}
	b, _ := json.Marshal(req)
	httpReq, err := http.NewRequest(http.MethodPost, c.httpBase+"/rpc", strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := hc.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out protocol.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("rpc error: %s", out.Error)
	}
	return out.Result, nil
}

// --- Bubble Tea commands ---

// FetchSessions returns a tea.Cmd that fetches session list.
func (c *IPCClient) FetchSessions() tea.Cmd {
	return func() tea.Msg {
		raw, err := c.Call(protocol.MethodSessionList, nil)
		if err != nil {
			return SessionListMsg{Err: err}
		}
		var sessions []types.Session
		if err := json.Unmarshal(raw, &sessions); err != nil {
			return SessionListMsg{Err: err}
		}
		return SessionListMsg{Sessions: sessions}
	}
}

// FetchHistory returns a tea.Cmd that fetches message history for a session.
func (c *IPCClient) FetchHistory(sessionID string) tea.Cmd {
	return func() tea.Msg {
		raw, err := c.Call(protocol.MethodSessionHistory, map[string]any{"sessionId": sessionID})
		if err != nil {
			return SessionHistoryMsg{SessionID: sessionID, Err: err}
		}
		var msgs []types.Message
		if err := json.Unmarshal(raw, &msgs); err != nil {
			return SessionHistoryMsg{SessionID: sessionID, Err: err}
		}
		return SessionHistoryMsg{SessionID: sessionID, Messages: msgs}
	}
}

// FetchAgents returns a tea.Cmd that fetches agent list.
func (c *IPCClient) FetchAgents() tea.Cmd {
	return func() tea.Msg {
		raw, err := c.Call(protocol.MethodAgentList, nil)
		if err != nil {
			return AgentListMsg{Err: err}
		}
		var agents []types.Agent
		if err := json.Unmarshal(raw, &agents); err != nil {
			return AgentListMsg{Err: err}
		}
		return AgentListMsg{Agents: agents}
	}
}

func (c *IPCClient) FetchUsage() tea.Cmd {
	return func() tea.Msg {
		raw, err := c.Call(protocol.MethodUsageGet, nil)
		if err != nil {
			return UsageMsg{Err: err}
		}
		var usage struct {
			PromptTokens     int64 `json:"promptTokens"`
			CompletionTokens int64 `json:"completionTokens"`
			TotalTokens      int64 `json:"totalTokens"`
			Calls            int64 `json:"calls"`
		}
		if err := json.Unmarshal(raw, &usage); err != nil {
			return UsageMsg{Err: err}
		}
		return UsageMsg{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
			Calls:            usage.Calls,
		}
	}
}

func (c *IPCClient) ConfigureProvider(name, baseURL, model, secret string) tea.Cmd {
	return func() tea.Msg {
		name = strings.TrimSpace(name)
		if name == "" {
			name = "openai"
		}
		model = strings.TrimSpace(model)
		if model == "" {
			model = "gpt-4o"
		}
		baseURL = strings.TrimSpace(baseURL)
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		secretID := name + "-key"
		if strings.TrimSpace(secret) != "" {
			if _, err := c.Call(protocol.MethodSecretPut, map[string]any{
				"id":    secretID,
				"value": secret,
			}); err != nil {
				return SetupMsg{Err: err}
			}
		}
		if _, err := c.Call(protocol.MethodProviderUpsert, map[string]any{
			"name":     name,
			"kind":     "openai-compat",
			"baseUrl":  baseURL,
			"models":   []string{model},
			"default":  true,
			"secretId": secretID,
		}); err != nil {
			return SetupMsg{Err: err}
		}
		return SetupMsg{Name: name, Model: model}
	}
}

// CreateSession returns a tea.Cmd that creates a new session.
func (c *IPCClient) CreateSession(agentID string) tea.Cmd {
	return func() tea.Msg {
		params := map[string]any{}
		if agentID != "" {
			params["agentId"] = agentID
		}
		raw, err := c.Call(protocol.MethodSessionCreate, params)
		if err != nil {
			return SessionListMsg{Err: err}
		}
		var sess types.Session
		if err := json.Unmarshal(raw, &sess); err != nil {
			return SessionListMsg{Err: err}
		}
		// Re-fetch session list
		raw2, err := c.Call(protocol.MethodSessionList, nil)
		if err != nil {
			return SessionListMsg{Sessions: []types.Session{sess}, CreatedSessionID: string(sess.ID)}
		}
		var sessions []types.Session
		if err := json.Unmarshal(raw2, &sessions); err != nil {
			return SessionListMsg{Sessions: []types.Session{sess}, CreatedSessionID: string(sess.ID)}
		}
		return SessionListMsg{Sessions: sessions, CreatedSessionID: string(sess.ID)}
	}
}

// SendMessage sends a chat message to a session.
func (c *IPCClient) SendMessage(sessionID, content string) tea.Cmd {
	return func() tea.Msg {
		_, err := c.Call(protocol.MethodSessionSend, map[string]any{
			"sessionId": sessionID,
			"content":   content,
		})
		return SendMsg{SessionID: sessionID, Err: err}
	}
}

// SubscribeEvents starts an SSE subscription in the background and sends
// EventMsg to the program via the provided send function.
func (c *IPCClient) SubscribeEvents(p *tea.Program) {
	go func() {
		hc := &http.Client{Timeout: 0}
		url := c.httpBase + "/events?token=" + c.token
		resp, err := hc.Get(url)
		if err != nil {
			p.Send(DisconnectedMsg{Err: err})
			return
		}
		defer resp.Body.Close()

		p.Send(ConnectedMsg{})
		br := bufio.NewReader(resp.Body)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				p.Send(DisconnectedMsg{Err: err})
				return
			}
			line = trimSSEData(line)
			if line == "" {
				continue
			}
			var ev protocol.EventFrame
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			p.Send(EventMsg{Frame: ev})
		}
	}()
}

// FetchProfiles fetches profile list via profile.list RPC.
func (c *IPCClient) FetchProfiles() tea.Cmd {
	return func() tea.Msg {
		raw, err := c.Call(protocol.MethodProfileList, nil)
		if err != nil {
			return ProfilesMsg{}
		}
		var profiles []types.AgentProfile
		if err := json.Unmarshal(raw, &profiles); err != nil {
			return ProfilesMsg{}
		}
		return ProfilesMsg{Profiles: profiles}
	}
}

// FetchLinkedSessions fetches sessions linked to a given sessionID via session.linked RPC.
func (c *IPCClient) FetchLinkedSessions(sessionID string) tea.Cmd {
	return func() tea.Msg {
		raw, err := c.Call(protocol.MethodSessionLinked, map[string]any{"sessionId": sessionID})
		if err != nil {
			return LinkedSessionsMsg{}
		}
		var result struct {
			Links    []types.SessionLink `json:"links"`
			Sessions []types.Session     `json:"sessions"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return LinkedSessionsMsg{}
		}
		return LinkedSessionsMsg{Links: result.Links, Sessions: result.Sessions}
	}
}

// LinkSessions links two sessions via session.link RPC.
func (c *IPCClient) LinkSessions(sessionA, sessionB, label string) tea.Cmd {
	return func() tea.Msg {
		raw, err := c.Call(protocol.MethodSessionLink, map[string]any{
			"sessionA": sessionA,
			"sessionB": sessionB,
			"label":    label,
		})
		if err != nil {
			return SessionLinkedMsg{}
		}
		var link types.SessionLink
		if err := json.Unmarshal(raw, &link); err != nil {
			return SessionLinkedMsg{}
		}
		return SessionLinkedMsg{Link: link}
	}
}

// UnlinkSessions unlinks two sessions via session.unlink RPC.
func (c *IPCClient) UnlinkSessions(linkID string) tea.Cmd {
	return func() tea.Msg {
		_, _ = c.Call(protocol.MethodSessionUnlink, map[string]any{"linkId": linkID})
		return nil
	}
}

// SetDefaultProfile sets a profile as default via profile.setDefault RPC.
func (c *IPCClient) SetDefaultProfile(profileID string) tea.Cmd {
	return func() tea.Msg {
		_, _ = c.Call(protocol.MethodProfileSetDefault, map[string]any{"profileId": profileID})
		// Re-fetch profiles after setting default
		raw, err := c.Call(protocol.MethodProfileList, nil)
		if err != nil {
			return ProfilesMsg{}
		}
		var profiles []types.AgentProfile
		if err := json.Unmarshal(raw, &profiles); err != nil {
			return ProfilesMsg{}
		}
		return ProfilesMsg{Profiles: profiles}
	}
}

// RelayMessage sends a message to a linked session via session.relay RPC.
func (c *IPCClient) RelayMessage(sessionID, content string) tea.Cmd {
	return func() tea.Msg {
		_, err := c.Call(protocol.MethodSessionRelay, map[string]any{
			"sessionId": sessionID,
			"content":   content,
		})
		return SendMsg{SessionID: sessionID, Err: err}
	}
}

// FetchAutomations fetches automation list via automation.list RPC.
func (c *IPCClient) FetchAutomations() tea.Cmd {
	return func() tea.Msg {
		raw, err := c.Call(protocol.MethodAutomationList, nil)
		if err != nil {
			return AutomationsMsg{Err: err}
		}
		var jobs []types.AutomationJob
		if err := json.Unmarshal(raw, &jobs); err != nil {
			return AutomationsMsg{Err: err}
		}
		return AutomationsMsg{Jobs: jobs}
	}
}

// RestoreCheckpoint restores the last checkpoint via checkpoint.restore RPC.
func (c *IPCClient) RestoreCheckpoint(sessionID string) tea.Cmd {
	return func() tea.Msg {
		params := map[string]any{}
		if sessionID != "" {
			params["sessionId"] = sessionID
		}
		_, err := c.Call(protocol.MethodCheckpointRestore, params)
		if err != nil {
			return SendMsg{SessionID: sessionID, Err: err}
		}
		return SendMsg{SessionID: sessionID}
	}
}

type AutomationsMsg struct {
	Jobs []types.AutomationJob
	Err  error
}

func trimSSEData(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "data:") {
		s = strings.TrimSpace(s[5:])
	}
	return s
}
