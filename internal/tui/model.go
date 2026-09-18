package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aether-dev/aether/internal/types"
	"github.com/aether-dev/aether/pkg/protocol"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Panel focus identifiers
type FocusPanel int

const (
	FocusSessions FocusPanel = iota
	FocusMessages
	FocusCode
	FocusPlan
	FocusInput
	FocusFiles
	_focusCount
)

// Model is the root Bubble Tea model.
type Model struct {
	// Panels
	sessionPanel  *SessionPanel
	fileTreePanel *FileTreePanel
	messagesPanel *MessagesPanel
	codePanel     *CodePanel
	planPanel     *PlanPanel
	inputBar      *InputBar
	pet           *Pet

	// IPC
	ipc           *IPCClient
	ipcErr        error
	connected     bool

	// State
	focus         FocusPanel
	currentSessID string
	agents        []types.Agent
	escCount      int
	escTime       time.Time
	lastStatus    string

	// Terminal dimensions
	width  int
	height int

	// Approval
	pendingApprovalID string

	// Ticker for refresh
	tickCount int
}

// NewModel creates the root model.
func NewModel(ipc *IPCClient) *Model {
	m := &Model{
		sessionPanel:  NewSessionPanel(),
		fileTreePanel: NewFileTreePanel(),
		messagesPanel: NewMessagesPanel(),
		codePanel:     NewCodePanel(),
		planPanel:     NewPlanPanel(),
		inputBar:      NewInputBar(),
		pet:           NewPet(),
		ipc:           ipc,
		focus:         FocusInput,
	}
	return m
}

// Init runs initial commands.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.ipc.FetchSessions(),
		m.ipc.FetchAgents(),
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg{t} }),
	)
}

type tickMsg struct{ t time.Time }

// Update handles all messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layoutPanels()

	case tea.KeyMsg:
		cmd := m.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tickMsg:
		m.tickCount++
		cmds = append(cmds, tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg{t} }))
		// Periodic refresh of sessions
		if m.tickCount%5 == 0 {
			cmds = append(cmds, m.ipc.FetchSessions())
		}

	case ConnectedMsg:
		m.connected = true
		m.lastStatus = styleDim.Render("● connected")

	case DisconnectedMsg:
		m.connected = false
		if msg.Err != nil {
			m.lastStatus = styleError.Render("✗ " + msg.Err.Error())
		} else {
			m.lastStatus = styleDim.Render("○ disconnected")
		}
		m.pet.SetMood(PetError)

	case SessionListMsg:
		if msg.Err != nil {
			m.ipcErr = msg.Err
			m.lastStatus = styleError.Render("sessions: " + msg.Err.Error())
		} else {
			m.sessionPanel.SetSessions(msg.Sessions)
			// Auto-select first if none selected
			if m.currentSessID == "" && len(msg.Sessions) > 0 {
				m.currentSessID = string(msg.Sessions[0].ID)
				cmds = append(cmds, m.ipc.FetchHistory(m.currentSessID))
			}
		}

	case SessionHistoryMsg:
		if msg.Err != nil {
			m.lastStatus = styleError.Render("history: " + msg.Err.Error())
		} else {
			m.messagesPanel.SetMessages(msg.Messages)
		}

	case AgentListMsg:
		if msg.Err == nil {
			m.agents = msg.Agents
		}

	case SendMsg:
		if msg.Err != nil {
			m.lastStatus = styleError.Render("send: " + msg.Err.Error())
			m.pet.SetMood(PetError)
		} else {
			m.pet.SetMood(PetRunning)
			m.lastStatus = styleDim.Render("● running…")
			// Refresh history after a moment
			cmds = append(cmds, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
				return refreshHistoryMsg{sessionID: msg.SessionID}
			}))
		}

	case refreshHistoryMsg:
		if msg.sessionID != "" {
			cmds = append(cmds, m.ipc.FetchHistory(msg.sessionID))
		}

	case EventMsg:
		cmd := m.handleEvent(msg.Frame)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// Forward key events to input bar when focused
	if m.focus == FocusInput {
		var tiCmd tea.Cmd
		newInput, tiCmd := m.inputBar.input.Update(msg)
		m.inputBar.input = newInput
		if tiCmd != nil {
			cmds = append(cmds, tiCmd)
		}
		// Check if / was typed to open slash menu
		val := m.inputBar.Value()
		if strings.HasPrefix(val, "/") && !m.inputBar.IsSlashOpen() {
			m.inputBar.OpenSlash()
		}
		m.inputBar.UpdateFilter(val)
	}

	return m, tea.Batch(cmds...)
}

type refreshHistoryMsg struct{ sessionID string }

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	// Global: Ctrl+C quit
	if msg.Type == tea.KeyCtrlC {
		return tea.Quit
	}

	// Double-Esc: cancel current run
	if msg.Type == tea.KeyEsc {
		now := time.Now()
		if now.Sub(m.escTime) < 800*time.Millisecond {
			m.escCount++
		} else {
			m.escCount = 1
		}
		m.escTime = now
		if m.escCount >= 2 {
			m.escCount = 0
			m.pet.SetMood(PetIdle)
			m.lastStatus = styleDim.Render("● stopped")
			// Close slash if open
			m.inputBar.CloseSlash()
			return nil
		}
		if m.inputBar.IsSlashOpen() {
			m.inputBar.CloseSlash()
			return nil
		}
		return nil
	}
	m.escCount = 0

	// Slash command navigation
	if m.inputBar.IsSlashOpen() {
		switch msg.Type {
		case tea.KeyUp:
			m.inputBar.SlashUp()
			return nil
		case tea.KeyDown:
			m.inputBar.SlashDown()
			return nil
		case tea.KeyEnter:
			name, ok := m.inputBar.SlashSelect()
			if ok && name == "/clear" {
				m.messagesPanel.messages = nil
				m.inputBar.Clear()
			}
			return nil
		case tea.KeyEsc:
			m.inputBar.CloseSlash()
			return nil
		}
	}

	// Approval card handling
	if m.messagesPanel.HasPendingCard() && (m.focus == FocusMessages || m.focus == FocusInput) {
		switch msg.String() {
		case "a", "A":
			cardID := m.pendingApprovalID
			m.messagesPanel.ClearPendingCard()
			m.pendingApprovalID = ""
			if cardID != "" {
				return m.approveCard(cardID, true)
			}
			return nil
		case "d", "D":
			cardID := m.pendingApprovalID
			m.messagesPanel.ClearPendingCard()
			m.pendingApprovalID = ""
			if cardID != "" {
				return m.approveCard(cardID, false)
			}
			return nil
		}
	}

	// Global panel switching
	switch msg.Type {
	case tea.KeyTab:
		m.focus = (m.focus + 1) % _focusCount
		m.updateFocus()
		return nil
	case tea.KeyCtrlS:
		m.focus = FocusCode
		m.updateFocus()
		return nil
	case tea.KeyCtrlE:
		m.focus = FocusFiles
		m.updateFocus()
		return nil
	case tea.KeyCtrlN:
		agentID := ""
		if len(m.agents) > 0 {
			agentID = string(m.agents[0].ID)
		}
		return m.ipc.CreateSession(agentID)
	case tea.KeyCtrlU:
		m.inputBar.Clear()
		return nil
	}

	// Enter: send message (when input focused)
	if msg.Type == tea.KeyEnter && m.focus == FocusInput {
		content := strings.TrimSpace(m.inputBar.Value())
		if content != "" && m.currentSessID != "" {
			m.inputBar.Clear()
			dm := DisplayMessage{
				Role:    types.RoleUser,
				Content: content,
				At:      time.Now(),
			}
			m.messagesPanel.AddMessage(dm)
			return m.ipc.SendMessage(m.currentSessID, content)
		}
		return nil
	}

	// Panel-specific keys
	switch m.focus {
	case FocusSessions:
		switch msg.Type {
		case tea.KeyUp:
			m.sessionPanel.MoveUp()
		case tea.KeyDown:
			m.sessionPanel.MoveDown()
		case tea.KeyEnter:
			if sess := m.sessionPanel.Selected(); sess != nil {
				m.currentSessID = string(sess.ID)
				return m.ipc.FetchHistory(m.currentSessID)
			}
		}
	case FocusFiles:
		switch msg.Type {
		case tea.KeyUp:
			m.fileTreePanel.MoveUp()
		case tea.KeyDown:
			m.fileTreePanel.MoveDown()
		case tea.KeyEnter:
			// Could load file into code panel
			f := m.fileTreePanel.Selected()
			if f != "" && !strings.HasSuffix(f, "/") {
				m.codePanel.SetFile(f, "")
				m.focus = FocusCode
				m.updateFocus()
			}
		}
	case FocusMessages:
		switch msg.Type {
		case tea.KeyUp:
			m.messagesPanel.ScrollUp()
		case tea.KeyDown:
			m.messagesPanel.ScrollDown()
		}
	case FocusCode:
		switch msg.Type {
		case tea.KeyUp:
			m.codePanel.ScrollUp()
		case tea.KeyDown:
			m.codePanel.ScrollDown()
		case tea.KeyRunes:
			if msg.String() == "d" {
				m.codePanel.ToggleDiff()
			}
		}
	}

	return nil
}

func (m *Model) handleEvent(ev protocol.EventFrame) tea.Cmd {
	switch types.EventType(ev.Type) {
	case types.EventMessageDelta:
		var payload struct {
			Content   string `json:"content"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			if payload.SessionID == m.currentSessID || payload.SessionID == "" {
				dm := DisplayMessage{
					Role:    types.RoleAssistant,
					Content: payload.Content,
					At:      time.Now(),
				}
				m.messagesPanel.AddMessage(dm)
				m.pet.SetMood(PetRunning)
			}
		}

	case types.EventMessageDone:
		m.pet.SetMood(PetDone)
		m.lastStatus = styleSuccess.Render("✓ done")
		// Refresh history to get full message
		if m.currentSessID != "" {
			return m.ipc.FetchHistory(m.currentSessID)
		}

	case types.EventToolStart:
		var payload struct {
			Name      string          `json:"name"`
			Args      json.RawMessage `json:"args"`
			SessionID string          `json:"sessionId"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			dm := DisplayMessage{
				Role:     types.RoleTool,
				ToolName: payload.Name,
				ToolArgs: string(payload.Args),
				At:       time.Now(),
			}
			// Extract file info from args
			if payload.Name == "read_file" || payload.Name == "write_file" || payload.Name == "patch_file" {
				var args struct {
					Path string `json:"path"`
				}
				if json.Unmarshal(payload.Args, &args) == nil && args.Path != "" {
					dm.ToolExtra = shortPath(args.Path)
					// Update code panel to show this file
					m.codePanel.SetFile(args.Path, "")
					m.lastStatus = styleDim.Render("● editing: " + shortPath(args.Path))
				}
			}
			m.messagesPanel.AddMessage(dm)
		}

	case types.EventToolResult:
		var payload struct {
			Name      string `json:"name"`
			Content   string `json:"content"`
			IsError   bool   `json:"isError"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			extra := summarizeToolResult(&types.ToolResult{
				Name:    payload.Name,
				Content: payload.Content,
				IsError: payload.IsError,
			})
			dm := DisplayMessage{
				Role:      types.RoleTool,
				ToolName:  payload.Name,
				ToolExtra: extra,
				IsError:   payload.IsError,
				At:        time.Now(),
			}
			m.messagesPanel.AddMessage(dm)
		}

	case types.EventAgentStatus:
		var payload struct {
			Status  string `json:"status"`
			AgentID string `json:"agentId"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			switch types.AgentStatus(payload.Status) {
			case types.AgentRunning:
				m.pet.SetMood(PetRunning)
				m.lastStatus = styleDim.Render("● agent running")
			case types.AgentIdle:
				m.pet.SetMood(PetDone)
				m.lastStatus = styleDim.Render("● agent idle")
			case types.AgentFailed:
				m.pet.SetMood(PetError)
				m.lastStatus = styleError.Render("✗ agent failed")
			}
		}

	case types.EventCardUpdated:
		// Could show approval card
		var payload struct {
			Card types.Card `json:"card"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			if payload.Card.ReviewState == types.ReviewPending {
				dm := &DisplayMessage{
					IsCard:  true,
					CardID:  string(payload.Card.ID),
					Content: payload.Card.Title + "\n" + payload.Card.Description,
				}
				m.messagesPanel.SetPendingCard(dm)
				m.pendingApprovalID = string(payload.Card.ID)
			}
		}

	case types.EventError:
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			dm := DisplayMessage{
				Role:    types.RoleAssistant,
				Content: "Error: " + payload.Error,
				IsError: true,
				At:      time.Now(),
			}
			m.messagesPanel.AddMessage(dm)
			m.pet.SetMood(PetError)
		}

	case types.EventSessionUpdated:
		return m.ipc.FetchSessions()
	}

	return nil
}

func (m *Model) approveCard(cardID string, approve bool) tea.Cmd {
	return func() tea.Msg {
		action := "approve"
		if !approve {
			action = "reject"
		}
		_, err := m.ipc.Call(protocol.MethodCardReview, map[string]any{
			"cardId": cardID,
			"action": action,
		})
		if err != nil {
			return EventMsg{Frame: protocol.EventFrame{
				Type:    string(types.EventError),
				Payload: json.RawMessage(`{"error":"` + err.Error() + `"}`),
			}}
		}
		return nil
	}
}

func (m *Model) updateFocus() {
	m.sessionPanel.SetActive(m.focus == FocusSessions)
	m.fileTreePanel.SetActive(m.focus == FocusFiles)
	m.messagesPanel.SetActive(m.focus == FocusMessages)
	m.codePanel.SetActive(m.focus == FocusCode)
	m.planPanel.SetActive(m.focus == FocusPlan)
	m.inputBar.SetActive(m.focus == FocusInput)
}

// layoutPanels distributes panel sizes based on terminal dimensions.
func (m *Model) layoutPanels() {
	w := m.width
	h := m.height

	// New layout: chat takes ~80%, right rail ~20%
	rightW := w * 22 / 100
	if rightW < 20 {
		rightW = 20
	}
	chatW := w - rightW

	inputH := 4
	mainH := h - inputH - 1
	if mainH < 10 {
		mainH = 10
	}

	planH := mainH * 65 / 100
	usageH := mainH - planH

	m.messagesPanel.SetSize(chatW, mainH)
	m.planPanel.SetSize(rightW, planH)
	m.inputBar.SetSize(w, inputH)

	// Keep unused panels at zero so they don't allocate/crash
	m.sessionPanel.SetSize(0, 0)
	m.fileTreePanel.SetSize(0, 0)
	m.codePanel.SetSize(0, 0)
	_ = usageH
}

// View renders the full TUI.
func (m *Model) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	m.updateFocus()

	w := m.width
	h := m.height

	// Layout: chat left (~78%), right rail (~22%)
	rightW := w * 22 / 100
	if rightW < 22 {
		rightW = 22
	}
	chatW := w - rightW

	inputH := 4
	mainH := h - inputH - 1
	if mainH < 10 {
		mainH = 10
	}

	planH := mainH * 65 / 100

	m.messagesPanel.SetSize(chatW, mainH)
	m.planPanel.SetSize(rightW, planH)
	m.inputBar.SetSize(w, inputH)

	// Render
	msgView  := m.messagesPanel.Render()
	planView := m.planPanel.Render()
	petView  := m.pet.Render(rightW)
	inputView := m.inputBar.Render()

	// Right rail: pet on top, plan below, usage at bottom
	rightRail := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().
			Width(rightW).
			Align(lipgloss.Center).
			Foreground(colorAgent).
			Render(petView),
		planView,
	)

	// Status line
	connMark := styleError.Render("○")
	if m.connected {
		connMark = styleSuccess.Render("●")
	}
	sessionInfo := ""
	if m.currentSessID != "" {
		sessionInfo = styleDim.Render(" sess:" + m.currentSessID[:min(8, len(m.currentSessID))])
	}
	statusLine := connMark + sessionInfo + "  " + m.lastStatus
	keybindHint := styleDim.Render(" Tab:focus  /: commands  Ctrl+N:new  Esc×2:stop  Ctrl+C:quit")

	mainRow := lipgloss.JoinHorizontal(lipgloss.Top,
		msgView,
		rightRail,
	)

	statusBar := lipgloss.NewStyle().
		Width(w).
		Foreground(colorDim).
		Render(fmt.Sprintf("%s  %s", statusLine, keybindHint))

	return lipgloss.JoinVertical(lipgloss.Left,
		mainRow,
		statusBar,
		inputView,
	)
}

func shortPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return "…/" + strings.Join(parts[len(parts)-2:], "/")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}