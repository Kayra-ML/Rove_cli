package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/types"
	"github.com/Kayra-ML/rove/pkg/client"
	"github.com/Kayra-ML/rove/pkg/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// IPCClient wraps the unix socket connection to the Rove Code daemon.
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

type WorkspaceListMsg struct {
	Workspaces []types.Workspace
	Err        error
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

// StreamGapMsg: the event stream came back without what fired while it was
// down; what the screen shows from events must be read again.
type StreamGapMsg struct{}

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

	tok := os.Getenv("ROVECODE_TOKEN")
	if tok == "" {
		tok = os.Getenv("AETHER_TOKEN")
	}
	if tok == "" {
		for _, tp := range config.TokenSearchPaths(cfg.DataDir) {
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

// callWait is how long an ordinary call may take to be answered (a
// variable so tests can shorten it).
var callWait = 30 * time.Second

// Call sends a JSON-RPC request over the unix socket (or TCP tunnel) and returns the raw result.
func (c *IPCClient) Call(method string, params any) (json.RawMessage, error) {
	return c.call(method, params, callWait)
}

// call is Call with its own wait: a chat turn is answered when it is done,
// which can be many minutes — after 30 seconds it was shown as failed while
// its reply went on arriving.
func (c *IPCClient) call(method string, params any, wait time.Duration) (json.RawMessage, error) {
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
		_ = conn.SetDeadline(time.Now().Add(wait))
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
		return c.callHTTP(req, wait)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(wait))

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

func (c *IPCClient) callHTTP(req protocol.Request, wait time.Duration) (json.RawMessage, error) {
	hc := &http.Client{Timeout: wait}
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

func (c *IPCClient) FetchWorkspaces() tea.Cmd {
	return func() tea.Msg {
		raw, err := c.Call(protocol.MethodWorkspaceList, nil)
		if err != nil {
			return WorkspaceListMsg{Err: err}
		}
		var list []types.Workspace
		if err := json.Unmarshal(raw, &list); err != nil {
			return WorkspaceListMsg{Err: err}
		}
		return WorkspaceListMsg{Workspaces: list}
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
		if cwd, err := os.Getwd(); err == nil && cwd != "" {
			params["cwd"] = cwd
			params["workspace"] = cwd
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
		_, err := c.call(protocol.MethodSessionSend, map[string]any{
			"sessionId": sessionID,
			"content":   content,
		}, protocol.MaxCallDuration)
		return SendMsg{SessionID: sessionID, Err: err}
	}
}

// Event stream reconnects: the first wait, and the longest.
const (
	streamRetry    = 500 * time.Millisecond
	streamRetryMax = 8 * time.Second
)

// SubscribeEvents follows the daemon's event stream in the background and
// hands its events to the program. A stream that drops — the daemon
// restarted, a tunnel blinked — is opened again from the last event seen;
// when the daemon cannot hand over what fired in between, the program is
// told (StreamGapMsg) to read its screen again.
func (c *IPCClient) SubscribeEvents(p *tea.Program) {
	go func() {
		last, wait, again := "", streamRetry, false
		for {
			opened, err := c.followEvents(&last, again, p.Send)
			p.Send(DisconnectedMsg{Err: err})
			if opened {
				wait = streamRetry
			}
			again = true
			time.Sleep(wait)
			if wait *= 2; wait > streamRetryMax {
				wait = streamRetryMax
			}
		}
	}()
}

// followEvents reads one stream until it ends. On a reconnect, a first
// frame that is not a hello that resumed (an older daemon sends none) means
// the events of the gap are gone.
func (c *IPCClient) followEvents(last *string, reconnect bool, send func(tea.Msg)) (opened bool, err error) {
	u := c.httpBase + "/events?token=" + url.QueryEscape(c.token)
	if *last != "" {
		u += "&since=" + url.QueryEscape(*last)
	}
	resp, err := (&http.Client{}).Get(u)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("events: %s", resp.Status)
	}
	send(ConnectedMsg{})
	first := true
	return true, client.ReadFrames(resp.Body, func(f client.Frame) {
		if first {
			first = false
			var h client.Hello
			resumed := f.Name == "hello" && json.Unmarshal([]byte(f.Data), &h) == nil && h.Resumed
			if reconnect && !resumed {
				send(StreamGapMsg{})
			}
		}
		if f.Name != "" {
			return
		}
		if f.ID != "" {
			*last = f.ID
		}
		var ev protocol.EventFrame
		if json.Unmarshal([]byte(f.Data), &ev) == nil {
			send(EventMsg{Frame: ev})
		}
	})
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
