package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aether-dev/aether/internal/sshtunnel"
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
	FocusProfiles
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
	profilePanel  *ProfilePanel
	sshPanel      *SSHPanel

	// IPC
	ipc       *IPCClient
	ipcErr    error
	connected bool

	// State
	focus         FocusPanel
	currentSessID string
	pendingPrompt string
	agents        []types.Agent
	escCount      int
	escTime       time.Time
	lastStatus    string

	// Terminal dimensions
	width  int
	height int

	// Approval
	pendingApprovalID string

	// Ticker for refresh + timer display
	tickCount int
	runStart  time.Time
	runActive bool
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
		profilePanel:  NewProfilePanel(),
		sshPanel:      NewSSHPanel(),
		ipc:           ipc,
		focus:         FocusInput,
	}
	return m
}

// SetInitialTunnel wires a pre-opened SSH tunnel (e.g. from --host flag) into the model.
func (m *Model) SetInitialTunnel(t *sshtunnel.Tunnel, alias string) {
	m.sshPanel.SetTunnel(t, alias)
}

func displayModel(agent types.Agent) string {
	model := strings.TrimSpace(agent.Model)
	provider := strings.TrimSpace(agent.Provider)
	if model == "" || model == "fake" {
		if provider == "" || provider == "fake" {
			return "no model configured"
		}
		return provider
	}
	if provider == "" || provider == "fake" || provider == model {
		return model
	}
	return provider + "/" + model
}

func (m *Model) SetStartupError(err error) {
	if err == nil {
		return
	}
	m.connected = false
	m.pet.SetMood(PetError)
	m.lastStatus = styleError.Render("✗ " + err.Error())
}

// Init runs initial commands.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.ipc.FetchSessions(),
		m.ipc.FetchAgents(),
		m.ipc.FetchProfiles(),
		m.ipc.FetchUsage(),
		tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{t} }),
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
		// Re-schedule at 500ms for smooth timer display
		cmds = append(cmds, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{t} }))
		// Periodic refresh of sessions every 10 ticks (5s)
		if m.tickCount%10 == 0 {
			cmds = append(cmds, m.ipc.FetchSessions())
		}
		// Periodic refresh of profiles every 60 ticks (30s)
		if m.tickCount%60 == 0 {
			cmds = append(cmds, m.ipc.FetchProfiles())
		}
		// Update run timer
		if m.runActive {
			elapsed := time.Since(m.runStart)
			mins := int(elapsed.Minutes())
			secs := int(elapsed.Seconds()) % 60
			tenths := int(elapsed.Milliseconds()/100) % 10
			m.planPanel.SetTimer(fmt.Sprintf("%02d:%02d.%d", mins, secs, tenths))
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
			sessionChanged := false
			if msg.CreatedSessionID != "" {
				m.currentSessID = msg.CreatedSessionID
				m.sessionPanel.SelectID(msg.CreatedSessionID)
				sessionChanged = true
			} else if m.currentSessID == "" && len(msg.Sessions) > 0 {
				m.currentSessID = string(msg.Sessions[0].ID)
				m.sessionPanel.SelectID(m.currentSessID)
				sessionChanged = true
			}

			if sessionChanged {
				m.messagesPanel.SetMessages(nil)
				if m.pendingPrompt != "" && m.currentSessID != "" {
					prompt := m.pendingPrompt
					m.pendingPrompt = ""
					m.messagesPanel.AddUserMessage(prompt)
					cmds = append(cmds,
						m.ipc.SendMessage(m.currentSessID, prompt),
						m.ipc.FetchLinkedSessions(m.currentSessID),
					)
				} else if m.currentSessID != "" {
					cmds = append(cmds,
						m.ipc.FetchHistory(m.currentSessID),
						m.ipc.FetchLinkedSessions(m.currentSessID),
					)
				}
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
			if len(msg.Agents) > 0 {
				m.planPanel.SetModelName(displayModel(msg.Agents[0]))
			}
		}

	case UsageMsg:
		if msg.Err == nil {
			m.planPanel.SetUsage(msg.PromptTokens, msg.CompletionTokens, msg.TotalTokens, msg.Calls)
		}

	case SendMsg:
		if msg.Err != nil {
			m.lastStatus = styleError.Render("send: " + msg.Err.Error())
			m.pet.SetMood(PetError)
		} else {
			m.pet.SetMood(PetRunning)
			m.planPanel.StartRun("Analyze request")
			m.runActive = true
			m.runStart = time.Now()
			m.lastStatus = styleDim.Render("● running…")
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

	case ProfilesMsg:
		m.profilePanel.SetProfiles(msg.Profiles)

	case LinkedSessionsMsg:
		m.sessionPanel.SetLinks(msg.Links)

	case SessionLinkedMsg:
		if msg.Link.ID != "" {
			m.lastStatus = styleDim.Render("↔ linked: " + string(msg.Link.Label))
		}
		if m.currentSessID != "" {
			cmds = append(cmds, m.ipc.FetchLinkedSessions(m.currentSessID))
		}

	case AutomationsMsg:
		if msg.Err != nil {
			m.lastStatus = styleError.Render("automations: " + msg.Err.Error())
		} else {
			var lines []string
			for _, j := range msg.Jobs {
				status := "disabled"
				if j.Enabled {
					status = "enabled"
				}
				lines = append(lines, fmt.Sprintf("  %s (%s)", j.Name, status))
			}
			if len(lines) == 0 {
				lines = append(lines, "  (no automations)")
			}
			dm := DisplayMessage{
				Role:    types.RoleAssistant,
				Content: "Automations:\n" + strings.Join(lines, "\n"),
				At:      time.Now(),
			}
			m.messagesPanel.AddMessage(dm)
		}

	case sshConnectMsg:
		m.ipc = msg.ipc
		m.sshPanel.SetTunnel(msg.tunnel, msg.alias)
		m.lastStatus = styleDim.Render("● ssh: " + msg.alias)
		m.currentSessID = ""
		cmds = append(cmds, m.ipc.FetchSessions(), m.ipc.FetchAgents())

	case sshStatusMsg:
		m.lastStatus = styleError.Render("ssh: " + msg.err)

	case sshDisconnectMsg:
		m.sshPanel.Disconnect()
		if ipc, err := NewIPCClient(); err == nil {
			m.ipc = ipc
		}
		m.lastStatus = styleDim.Render("○ local")
		m.currentSessID = ""
		cmds = append(cmds, m.ipc.FetchSessions(), m.ipc.FetchAgents())
	}

	// Forward key events to input bar when focused
	if m.focus == FocusInput {
		var tiCmd tea.Cmd
		newInput, tiCmd := m.inputBar.input.Update(msg)
		m.inputBar.input = newInput
		if tiCmd != nil {
			cmds = append(cmds, tiCmd)
		}
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

	// Esc: cancel current run (single press)
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
			m.runActive = false
			m.planPanel.SetTimer("")
			m.lastStatus = styleDim.Render("● stopped")
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
			if ok {
				switch name {
				case "/clear":
					m.messagesPanel.messages = nil
					m.inputBar.Clear()
				case "/undo":
					m.inputBar.Clear()
					return m.ipc.RestoreCheckpoint(m.currentSessID)
				case "/automations":
					m.inputBar.Clear()
					return m.ipc.FetchAutomations()
				case "/run":
					m.inputBar.Clear()
					m.inputBar.input.SetValue("/run ")
				case "/ssh":
					m.inputBar.Clear()
					m.sshPanel.Open()
				case "/ssh-connect":
					m.inputBar.Clear()
					m.sshPanel.Open()
				case "/ssh-disconnect":
					m.inputBar.Clear()
					return m.disconnectSSH()
				}
			}
			return nil
		case tea.KeyEsc:
			m.inputBar.CloseSlash()
			return nil
		}
	}

	// SSH panel key handling
	if m.sshPanel.IsOpen() {
		return m.handleSSHPanelKey(msg)
	}

	// Profile panel key handling
	if m.profilePanel.IsOpen() {
		if m.profilePanel.IsEnteringName() {
			switch msg.Type {
			case tea.KeyEnter:
				name := m.profilePanel.CommitNewProfile()
				if name != "" {
					return func() tea.Msg {
						_, _ = m.ipc.Call("profile.upsert", map[string]any{
							"name": name,
							"role": "developer",
						})
						raw, err := m.ipc.Call("profile.list", nil)
						if err != nil {
							return ProfilesMsg{}
						}
						var profiles []types.AgentProfile
						if err2 := json.Unmarshal(raw, &profiles); err2 != nil {
							return ProfilesMsg{}
						}
						return ProfilesMsg{Profiles: profiles}
					}
				}
				return nil
			case tea.KeyEsc:
				m.profilePanel.CancelNewProfile()
				return nil
			case tea.KeyBackspace:
				m.profilePanel.BackspaceName()
				return nil
			case tea.KeyRunes:
				for _, r := range msg.Runes {
					m.profilePanel.AppendNameChar(r)
				}
				return nil
			}
			return nil
		}
		switch msg.Type {
		case tea.KeyUp:
			m.profilePanel.MoveUp()
			return nil
		case tea.KeyDown:
			m.profilePanel.MoveDown()
			return nil
		case tea.KeyEnter:
			if id := m.profilePanel.SelectedID(); id != "" {
				return m.ipc.SetDefaultProfile(id)
			}
			return nil
		case tea.KeyEsc:
			m.profilePanel.Close()
			return nil
		case tea.KeyRunes:
			switch msg.String() {
			case "n":
				m.profilePanel.StartNewProfile()
			case "d":
				if id := m.profilePanel.SelectedID(); id != "" {
					return func() tea.Msg {
						_, _ = m.ipc.Call("profile.delete", map[string]any{"profileId": id})
						return m.ipc.FetchProfiles()()
					}
				}
			}
			return nil
		}
		return nil
	}

	// Session panel inline prompt handling
	if m.sessionPanel.IsPromptOpen() {
		switch msg.Type {
		case tea.KeyEnter:
			if m.sessionPanel.IsLinkPromptOpen() {
				targetID := m.sessionPanel.CommitLinkPrompt()
				if targetID != "" && m.currentSessID != "" {
					return m.ipc.LinkSessions(m.currentSessID, targetID, "")
				}
			} else if m.sessionPanel.IsRelayPromptOpen() {
				content := m.sessionPanel.CommitRelayPrompt()
				if content != "" && m.currentSessID != "" {
					return m.ipc.RelayMessage(m.currentSessID, content)
				}
			}
			return nil
		case tea.KeyEsc:
			m.sessionPanel.ClosePrompts()
			return nil
		case tea.KeyBackspace:
			m.sessionPanel.BackspacePrompt()
			return nil
		case tea.KeyRunes:
			for _, r := range msg.Runes {
				m.sessionPanel.AppendPromptChar(r)
			}
			return nil
		}
		return nil
	}

	// Approval card handling
	if m.messagesPanel.HasPendingCard() && (m.focus == FocusMessages || m.focus == FocusInput) {
		switch msg.String() {
		case "a", "A":
			cardID := m.pendingApprovalID
			m.messagesPanel.ClearPendingCard()
			m.pendingApprovalID = ""
			m.planPanel.SetNeedsApproval(false)
			if cardID != "" {
				return m.approveCard(cardID, true)
			}
			return nil
		case "d", "D":
			cardID := m.pendingApprovalID
			m.messagesPanel.ClearPendingCard()
			m.pendingApprovalID = ""
			m.planPanel.SetNeedsApproval(false)
			if cardID != "" {
				return m.approveCard(cardID, false)
			}
			return nil
		}
	}

	// Global panel switching
	switch msg.Type {
	case tea.KeyCtrlH:
		if m.sshPanel.IsOpen() {
			m.sshPanel.Close()
		} else {
			m.sshPanel.Open()
		}
		return nil
	case tea.KeyCtrlK:
		// Open slash command palette
		m.inputBar.OpenSlash()
		return nil
	case tea.KeyCtrlP:
		m.profilePanel.Toggle()
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
	case tea.KeyTab:
		// Only cycle between Messages and Input (no left rail anymore)
		if m.focus == FocusInput {
			m.focus = FocusMessages
		} else {
			m.focus = FocusInput
		}
		m.updateFocus()
		return nil
	}

	// Enter: send immediately, creating the first session transparently.
	if msg.Type == tea.KeyEnter && m.focus == FocusInput {
		content := strings.TrimSpace(m.inputBar.Value())
		if content == "" {
			return nil
		}
		m.inputBar.Clear()
		if m.currentSessID == "" {
			m.pendingPrompt = content
			m.lastStatus = styleDim.Render("● creating session…")
			agentID := ""
			if len(m.agents) > 0 {
				agentID = string(m.agents[0].ID)
			}
			return m.ipc.CreateSession(agentID)
		}
		m.messagesPanel.AddUserMessage(content)
		return m.ipc.SendMessage(m.currentSessID, content)
	}

	// Panel-specific keys
	switch m.focus {
	case FocusMessages:
		switch msg.Type {
		case tea.KeyUp:
			m.messagesPanel.ScrollUp()
		case tea.KeyDown:
			m.messagesPanel.ScrollDown()
		}
	}

	return nil
}

func (m *Model) handleEvent(ev protocol.EventFrame) tea.Cmd {
	switch types.EventType(ev.Type) {
	case types.EventMessageDelta:
		var payload struct {
			Content   string `json:"content"`
			Delta     string `json:"delta"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			text := payload.Delta
			if text == "" {
				text = payload.Content
			}
			if payload.SessionID == m.currentSessID || payload.SessionID == "" {
				m.messagesPanel.AppendAssistantDelta(text)
				m.planPanel.AdvanceRun("Compose response")
				m.pet.SetMood(PetRunning)
			}
		}

	case types.EventMessageDone:
		m.pet.SetMood(PetDone)
		m.runActive = false
		m.planPanel.CompleteRun()
		m.planPanel.SetTimer("")
		m.lastStatus = styleSuccess.Render("✓ done")
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
			if payload.Name == "read_file" || payload.Name == "write_file" || payload.Name == "patch_file" {
				var args struct {
					Path string `json:"path"`
				}
				if json.Unmarshal(payload.Args, &args) == nil && args.Path != "" {
					dm.ToolExtra = shortPath(args.Path)
					m.codePanel.SetFile(args.Path, "")
					m.lastStatus = styleDim.Render("● " + shortPath(args.Path))
				}
			}
			m.messagesPanel.AddMessage(dm)
			step := shortenToolName(payload.Name)
			if dm.ToolExtra != "" {
				step += " " + dm.ToolExtra
			}
			m.planPanel.AdvanceRun(step)
		}

	case types.EventToolResult:
		var payload struct {
			Name      string `json:"name"`
			Content   string `json:"content"`
			IsError   bool   `json:"isError"`
			Error     bool   `json:"error"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			isError := payload.IsError || payload.Error
			extra := summarizeToolResult(&types.ToolResult{
				Name:    payload.Name,
				Content: payload.Content,
				IsError: isError,
			})
			m.messagesPanel.CompleteTool(payload.Name, extra, isError)
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
				m.runActive = true
				if m.runStart.IsZero() {
					m.runStart = time.Now()
				}
				m.lastStatus = styleDim.Render("● agent running")
			case types.AgentIdle:
				m.pet.SetMood(PetDone)
				m.runActive = false
				m.planPanel.CompleteRun()
				m.lastStatus = styleDim.Render("● agent idle")
				return m.ipc.FetchUsage()
			case types.AgentFailed:
				m.pet.SetMood(PetError)
				m.runActive = false
				m.lastStatus = styleError.Render("✗ agent failed")
				return m.ipc.FetchUsage()
			}
		}

	case types.EventCardUpdated:
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
				m.planPanel.SetNeedsApproval(true)
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

type tuiLayout struct {
	topH, statusH, inputH int
	mainH                 int
	chatW, rightW         int
	dividerW              int
	showRail              bool
}

func (m *Model) calculateLayout() tuiLayout {
	layout := tuiLayout{topH: 1, statusH: 1, inputH: m.inputBar.DesiredHeight()}
	layout.mainH = m.height - layout.topH - layout.statusH - layout.inputH
	if layout.mainH < 4 {
		layout.mainH = 4
	}
	layout.showRail = m.width >= 88 && layout.mainH >= 14
	if layout.showRail {
		layout.dividerW = 1
		layout.rightW = clampInt(m.width*24/100, 28, 36)
		layout.chatW = m.width - layout.rightW - layout.dividerW
	} else {
		layout.chatW = m.width
	}
	return layout
}

// layoutPanels keeps every rendered surface on the same geometry source.
func (m *Model) layoutPanels() {
	layout := m.calculateLayout()
	m.messagesPanel.SetSize(layout.chatW, layout.mainH)
	m.planPanel.SetSize(layout.rightW, layout.mainH)
	m.inputBar.SetSize(m.width, layout.inputH)
	m.sessionPanel.SetSize(0, 0)
	m.fileTreePanel.SetSize(0, 0)
	m.codePanel.SetSize(0, 0)
}

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < 52 || m.height < 12 {
		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			styleBrand.Render("ROVE CODE")+"\n"+styleMeta.Render("terminal needs at least 52×12"),
		)
	}

	m.updateFocus()
	m.layoutPanels()
	layout := m.calculateLayout()

	topBar := m.renderTopBar(m.width)
	messages := m.messagesPanel.Render()
	mainRow := messages
	if layout.showRail {
		divider := lipgloss.NewStyle().
			Width(1).
			Height(layout.mainH).
			Foreground(colorSep).
			Background(colorBgPanel).
			Render(strings.TrimSuffix(strings.Repeat("│\n", layout.mainH), "\n"))
		mainRow = lipgloss.JoinHorizontal(lipgloss.Top, messages, divider, m.planPanel.Render())
	}

	base := lipgloss.JoinVertical(
		lipgloss.Left,
		topBar,
		mainRow,
		m.renderStatusBar(m.width),
		m.inputBar.Render(),
	)

	if m.profilePanel.IsOpen() {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.profilePanel.Render())
	}
	if m.sshPanel.IsOpen() {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.sshPanel.Render())
	}
	return base
}

func (m *Model) renderTopBar(w int) string {
	brand := styleBrandMark.Render("◆") + " " + styleBrand.Render("ROVE CODE")
	var rightParts []string
	if m.sshPanel.TunnelStatusText() != "" {
		rightParts = append(rightParts, m.sshPanel.TunnelStatusText())
	} else {
		rightParts = append(rightParts, "local")
	}
	if profile := m.profilePanel.ActiveProfileName(); profile != "" {
		rightParts = append(rightParts, profile)
	}
	if m.pet != nil {
		rightParts = append(rightParts, stripSimpleANSI(m.pet.Render(0)))
	}
	right := styleMuted.Render(strings.Join(rightParts, "  ·  "))
	gap := w - lipgloss.Width(brand) - lipgloss.Width(right) - 4
	if gap < 1 {
		right = styleMuted.Render(stripSimpleANSI(m.pet.Render(0)))
		gap = w - lipgloss.Width(brand) - lipgloss.Width(right) - 4
	}
	if gap < 1 {
		gap = 1
	}
	line := "  " + brand + strings.Repeat(" ", gap) + right + "  "
	return lipgloss.NewStyle().Width(w).Background(colorBgPanel).Render(fitVisible(line, w))
}

func (m *Model) renderStatusBar(w int) string {
	shortcut := func(key, label string) string {
		return lipgloss.NewStyle().Foreground(colorWhite).Render(key) + " " + styleMeta.Render(label)
	}
	left := strings.Join([]string{
		shortcut("esc", "stop"),
		shortcut("^k", "commands"),
		shortcut("^n", "new"),
		shortcut("^p", "profiles"),
		shortcut("^h", "ssh"),
	}, "   ")

	right := m.lastStatus
	if right == "" {
		if m.connected {
			right = styleSuccess.Render("● connected")
		} else {
			right = styleError.Render("○ offline")
		}
	}
	if m.currentSessID != "" {
		right = styleMeta.Render("session "+m.currentSessID[:min(7, len(m.currentSessID))]) + "   " + right
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 4
	if gap < 1 {
		right = ""
		gap = w - lipgloss.Width(left) - 4
	}
	if gap < 1 {
		gap = 1
	}
	line := "  " + left + strings.Repeat(" ", gap) + right + "  "
	return styleStatusBar.Width(w).Render(fitVisible(line, w))
}

func shortPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return "…/" + strings.Join(parts[len(parts)-2:], "/")
}

// overlayModal centers a modal string over a base string (already rendered lines).
func overlayModal(base, modal string, w, h int) string {
	modalLines := strings.Split(modal, "\n")
	modalH := len(modalLines)
	modalW := 0
	for _, l := range modalLines {
		if lw := lipgloss.Width(l); lw > modalW {
			modalW = lw
		}
	}
	topPad := (h - modalH) / 2
	if topPad < 0 {
		topPad = 0
	}
	leftPad := (w - modalW) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < h {
		baseLines = append(baseLines, strings.Repeat(" ", w))
	}
	for i, ml := range modalLines {
		row := topPad + i
		if row >= len(baseLines) {
			break
		}
		bl := baseLines[row]
		blRunes := []rune(bl)
		for len(blRunes) < w {
			blRunes = append(blRunes, ' ')
		}
		mlRunes := []rune(ml)
		end := leftPad + len(mlRunes)
		if end > len(blRunes) {
			end = len(blRunes)
		}
		copy(blRunes[leftPad:end], mlRunes[:end-leftPad])
		baseLines[row] = string(blRunes)
	}
	return strings.Join(baseLines, "\n")
}

// handleSSHPanelKey processes keystrokes when the SSH panel is open.
func (m *Model) handleSSHPanelKey(msg tea.KeyMsg) tea.Cmd {
	if m.sshPanel.IsAddMode() {
		switch msg.Type {
		case tea.KeyEsc:
			m.sshPanel.CancelAdd()
			return nil
		case tea.KeyTab, tea.KeyEnter:
			m.sshPanel.NextAddField()
			return nil
		default:
			switch m.sshPanel.addField {
			case 0:
				newM, cmd := m.sshPanel.addAlias.Update(msg)
				m.sshPanel.addAlias = newM
				return cmd
			case 1:
				newM, cmd := m.sshPanel.addSpec.Update(msg)
				m.sshPanel.addSpec = newM
				return cmd
			case 2:
				newM, cmd := m.sshPanel.addNote.Update(msg)
				m.sshPanel.addNote = newM
				return cmd
			}
		}
		return nil
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.sshPanel.Close()
		return nil
	case tea.KeyUp:
		m.sshPanel.MoveUp()
	case tea.KeyDown:
		m.sshPanel.MoveDown()
	case tea.KeyEnter:
		h := m.sshPanel.SelectedHost()
		if h == nil {
			return nil
		}
		return m.connectSSHHost(h.Alias, h.Spec)
	case tea.KeyRunes:
		switch msg.String() {
		case "a":
			m.sshPanel.StartAdd()
		case "d":
			m.sshPanel.DeleteSelected()
		case "x":
			return m.disconnectSSH()
		}
	}
	return nil
}

// sshConnectMsg is sent after successfully establishing an SSH tunnel.
type sshConnectMsg struct {
	alias  string
	tunnel *sshtunnel.Tunnel
	ipc    *IPCClient
}

// connectSSHHost opens an SSH tunnel to the named host and switches IPC.
func (m *Model) connectSSHHost(alias, spec string) tea.Cmd {
	return func() tea.Msg {
		t, err := sshtunnel.NewTunnel(spec)
		if err != nil {
			return sshStatusMsg{err: err.Error()}
		}
		if err := t.Open(); err != nil {
			return sshStatusMsg{err: "tunnel: " + err.Error()}
		}
		tok, _ := t.FetchRemoteToken()
		ipc := NewIPCClientTCP(t.LocalAddr(), tok)
		return sshConnectMsg{alias: alias, tunnel: t, ipc: ipc}
	}
}

type sshStatusMsg struct{ err string }

// disconnectSSH closes the tunnel and reconnects to local socket.
func (m *Model) disconnectSSH() tea.Cmd {
	return func() tea.Msg {
		return sshDisconnectMsg{}
	}
}

type sshDisconnectMsg struct{}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
