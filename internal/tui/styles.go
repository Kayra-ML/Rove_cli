package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Night palette: restrained contrast, warm execution states, no neon boxes.
var (
	colorBg       = lipgloss.Color("#08090D")
	colorBgPanel  = lipgloss.Color("#0D0F15")
	colorElevated = lipgloss.Color("#12151D")
	colorWhite    = lipgloss.Color("#E7E9EE")
	colorMid      = lipgloss.Color("#858A9A")
	colorDim      = lipgloss.Color("#555B6D")
	colorSep      = lipgloss.Color("#242833")

	colorGreen  = lipgloss.Color("#6BD88D")
	colorYellow = lipgloss.Color("#E5B567")
	colorOrange = lipgloss.Color("#E58B5B")
	colorRed    = lipgloss.Color("#E56B6F")
	colorBlue   = lipgloss.Color("#7AA2F7")
	colorViolet = lipgloss.Color("#A78BFA")

	// Compatibility aliases used by the other TUI surfaces.
	colorUser         = colorBlue
	colorAgent        = colorWhite
	colorTool         = colorMid
	colorApproval     = colorYellow
	colorError        = colorRed
	colorSuccess      = colorGreen
	colorHighlight    = colorYellow
	colorContextBar   = colorBlue
	colorBorderDim    = colorSep
	colorBorderActive = colorMid
	colorPet          = colorDim
	colorSelected     = lipgloss.Color("#1A1F2B")

	stylePanelBorder       = lipgloss.NewStyle().Background(colorBg)
	stylePanelBorderActive = lipgloss.NewStyle().Background(colorBg)

	styleTitle       = lipgloss.NewStyle().Foreground(colorMid)
	styleTitleActive = lipgloss.NewStyle().Foreground(colorWhite)
	styleBrand       = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	styleBrandMark   = lipgloss.NewStyle().Foreground(colorViolet).Bold(true)
	styleMeta        = lipgloss.NewStyle().Foreground(colorDim)

	styleUserMsg  = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	styleAgentMsg = lipgloss.NewStyle().Foreground(colorWhite)
	styleToolRow  = lipgloss.NewStyle().Foreground(colorMid)
	styleToolEdit = lipgloss.NewStyle().Foreground(colorMid)

	styleApprovalRow    = lipgloss.NewStyle().Foreground(colorYellow).Background(colorElevated)
	styleApprovalBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorYellow).
				Padding(0, 1)

	styleError       = lipgloss.NewStyle().Foreground(colorRed)
	styleSuccess     = lipgloss.NewStyle().Foreground(colorGreen)
	styleDim         = lipgloss.NewStyle().Foreground(colorDim)
	styleMuted       = lipgloss.NewStyle().Foreground(colorMid)
	styleHighlight   = lipgloss.NewStyle().Foreground(colorYellow)
	styleSelected    = lipgloss.NewStyle().Background(colorSelected).Foreground(colorWhite)
	stylePet         = lipgloss.NewStyle().Foreground(colorDim)
	styleSep         = lipgloss.NewStyle().Foreground(colorSep)
	styleStatusBar   = lipgloss.NewStyle().Foreground(colorDim).Background(colorBgPanel)
	styleStatusRight = lipgloss.NewStyle().Foreground(colorMid).Background(colorBgPanel)
)

func panelStyle(active bool) lipgloss.Style {
	return stylePanelBorder
}

func titleStyle(active bool) lipgloss.Style {
	if active {
		return styleTitleActive
	}
	return styleTitle
}

// ruledHeader renders a title embedded into a single hairline.
func ruledHeader(label string, width int, right string, active bool) string {
	if width <= 0 {
		return ""
	}
	left := "─ " + titleStyle(active).Render(label) + " "
	rightText := ""
	if right != "" {
		rightText = " " + styleMeta.Render(right) + " ─"
	}
	used := lipgloss.Width(left) + lipgloss.Width(rightText)
	fill := width - used
	if fill < 0 {
		fill = 0
	}
	line := styleSep.Render("─") + " " + titleStyle(active).Render(label) + " "
	line += styleSep.Render(strings.Repeat("─", fill))
	if rightText != "" {
		line += " " + styleMeta.Render(right) + " " + styleSep.Render("─")
	}
	return fitVisible(line, width)
}

func fitVisible(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s + strings.Repeat(" ", width-lipgloss.Width(s))
	}
	// ANSI-aware hard truncation is deliberately avoided. Callers only pass
	// compact labels here; a plain-rune fallback keeps malformed escapes out.
	plain := []rune(stripSimpleANSI(s))
	if len(plain) > width {
		plain = plain[:width]
	}
	return string(plain) + strings.Repeat(" ", width-len(plain))
}

// stripSimpleANSI handles SGR sequences emitted by lipgloss for rare narrow
// terminal fallbacks. Normal-width paths preserve styled text untouched.
func stripSimpleANSI(s string) string {
	var out strings.Builder
	inEscape := false
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !inEscape && b == 0x1b {
			inEscape = true
			continue
		}
		if inEscape {
			if b == 'm' {
				inEscape = false
			}
			continue
		}
		out.WriteByte(b)
	}
	return out.String()
}

func clampInt(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
