package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorBorderDim    = lipgloss.Color("#444444")
	colorBorderActive = lipgloss.Color("#ffffff")
	colorUser         = lipgloss.Color("#00cccc")
	colorAgent        = lipgloss.Color("#ffffff")
	colorTool         = lipgloss.Color("#aaaa00")
	colorApproval     = lipgloss.Color("#ffff00")
	colorError        = lipgloss.Color("#ff4444")
	colorSuccess      = lipgloss.Color("#44ff44")
	colorContextBar   = lipgloss.Color("#5555ff")
	colorPet          = lipgloss.Color("#ffffff")
	colorDim          = lipgloss.Color("#888888")
	colorHighlight    = lipgloss.Color("#ffaa00")
	colorSelected     = lipgloss.Color("#333366")

	stylePanelBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorderDim)

	stylePanelBorderActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorderActive)

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorDim).
			PaddingLeft(1)

	styleTitleActive = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBorderActive).
				PaddingLeft(1)

	styleUserMsg = lipgloss.NewStyle().
			Foreground(colorUser)

	styleAgentMsg = lipgloss.NewStyle().
			Foreground(colorAgent)

	styleToolRow = lipgloss.NewStyle().
			Foreground(colorTool)

	styleApprovalBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorApproval).
				Padding(0, 1)

	styleError = lipgloss.NewStyle().
			Foreground(colorError)

	styleSuccess = lipgloss.NewStyle().
			Foreground(colorSuccess)

	styleDim = lipgloss.NewStyle().
			Foreground(colorDim)

	styleHighlight = lipgloss.NewStyle().
			Foreground(colorHighlight).
			Background(lipgloss.Color("#332200"))

	styleSelected = lipgloss.NewStyle().
			Background(colorSelected).
			Foreground(colorAgent)

	stylePet = lipgloss.NewStyle().
			Foreground(colorPet).
			Bold(true)
)

func panelStyle(active bool) lipgloss.Style {
	if active {
		return stylePanelBorderActive
	}
	return stylePanelBorder
}

func titleStyle(active bool) lipgloss.Style {
	if active {
		return styleTitleActive
	}
	return styleTitle
}