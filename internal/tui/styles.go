package tui

import "github.com/charmbracelet/lipgloss"

// ── Palette ──────────────────────────────────────────────────────────────────

var (
	// Backgrounds
	colorBg      = lipgloss.Color("#0a0a0f")
	colorBgPanel = lipgloss.Color("#0d0d14")

	// Foregrounds
	colorWhite   = lipgloss.Color("#e8e8f0")
	colorDim     = lipgloss.Color("#4a4a5a")
	colorMid     = lipgloss.Color("#7a7a8a")
	colorSep     = lipgloss.Color("#1e1e2a")

	// Semantic
	colorGreen   = lipgloss.Color("#3dcc7a")   // done / success
	colorYellow  = lipgloss.Color("#f0b840")   // active / warning / approval
	colorOrange  = lipgloss.Color("#e87040")   // high-priority
	colorRed     = lipgloss.Color("#e84040")   // error / deleted
	colorBlue    = lipgloss.Color("#4888cc")   // context bar fill

	// Aliases kept for compatibility
	colorUser      = colorWhite
	colorAgent     = colorWhite
	colorTool      = colorMid
	colorApproval  = colorYellow
	colorError     = colorRed
	colorSuccess   = colorGreen
	colorHighlight = colorYellow
	colorContextBar = colorBlue
	colorBorderDim = colorSep
	colorBorderActive = colorMid
	colorPet       = colorDim
	colorSelected  = lipgloss.Color("#1a1a2e")

	// ── Styles ────────────────────────────────────────────────────────────

	// No-border panel: just flat background, no box-drawing
	stylePanelBorder = lipgloss.NewStyle().
		Background(colorBg)

	stylePanelBorderActive = lipgloss.NewStyle().
		Background(colorBg)

	// Panel header: dim all-caps label, thin separator below via rendering
	styleTitle = lipgloss.NewStyle().
		Foreground(colorDim).
		Bold(false)

	styleTitleActive = lipgloss.NewStyle().
		Foreground(colorMid).
		Bold(false)

	// Messages
	styleUserMsg = lipgloss.NewStyle().
		Foreground(colorWhite).
		Bold(true)

	styleAgentMsg = lipgloss.NewStyle().
		Foreground(colorWhite)

	styleToolRow = lipgloss.NewStyle().
		Foreground(colorMid)

	styleToolEdit = lipgloss.NewStyle().
		Foreground(colorMid)

	// Approval card row — yellow background highlight
	styleApprovalRow = lipgloss.NewStyle().
		Foreground(colorBg).
		Background(colorYellow).
		Bold(true)

	// Kept for modal overlays (profiles, ssh)
	styleApprovalBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorYellow).
		Padding(0, 1)

	styleError = lipgloss.NewStyle().
		Foreground(colorRed)

	styleSuccess = lipgloss.NewStyle().
		Foreground(colorGreen)

	styleDim = lipgloss.NewStyle().
		Foreground(colorDim)

	styleHighlight = lipgloss.NewStyle().
		Foreground(colorYellow)

	styleSelected = lipgloss.NewStyle().
		Background(colorSelected).
		Foreground(colorWhite)

	stylePet = lipgloss.NewStyle().
		Foreground(colorDim)

	// Separator line style
	styleSep = lipgloss.NewStyle().
		Foreground(colorSep)

	// Status bar
	styleStatusBar = lipgloss.NewStyle().
		Foreground(colorDim).
		Background(colorBg)

	styleStatusRight = lipgloss.NewStyle().
		Foreground(colorDim).
		Background(colorBg)
)

func panelStyle(active bool) lipgloss.Style {
	// No borders — flat panels
	return stylePanelBorder
}

func titleStyle(active bool) lipgloss.Style {
	if active {
		return styleTitleActive
	}
	return styleTitle
}