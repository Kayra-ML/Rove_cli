package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Rove Code night palette — restrained contrast, warm execution states.
var (
	colorBg       = lipgloss.Color("#08090D")
	colorBgPanel  = lipgloss.Color("#0D0F15")
	colorElevated = lipgloss.Color("#12151D")
	colorWhite    = lipgloss.Color("#E7E9EE")
	colorMid      = lipgloss.Color("#858A9A")
	colorDim      = lipgloss.Color("#3E4455")
	colorSep      = lipgloss.Color("#1E2130")
	colorFrame    = lipgloss.Color("#2A2F42")

	colorGreen  = lipgloss.Color("#6BD88D")
	colorYellow = lipgloss.Color("#E5B567")
	colorOrange = lipgloss.Color("#E58B5B")
	colorRed    = lipgloss.Color("#E56B6F")
	colorBlue   = lipgloss.Color("#7AA2F7")
	colorViolet = lipgloss.Color("#A78BFA")
	colorCyan   = lipgloss.Color("#56C9C9")

	// Compatibility aliases used by other TUI surfaces.
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
	styleFrame       = lipgloss.NewStyle().Foreground(colorFrame)
	styleStatusBar   = lipgloss.NewStyle().Foreground(colorDim).Background(colorBgPanel)
	styleStatusRight = lipgloss.NewStyle().Foreground(colorMid).Background(colorBgPanel)

	// Plan item state icons.
	iconDone    = styleSuccess.Render("◆")
	iconCurrent = styleHighlight.Bold(true).Render("◈")
	iconPending = styleDim.Render("◇")
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

// sectionHeader renders a panel section title: "-- label ------------- meta -"
func sectionHeader(label string, width int, right string, active bool) string {
	if width <= 0 {
		return ""
	}
	labelRendered := " " + titleStyle(active).Render(label) + " "
	rightText := ""
	if right != "" {
		rightText = " " + styleMeta.Render(right) + " " + styleFrame.Render(frameCharH)
	}
	prefixW := 2 + lipgloss.Width(labelRendered) // "--" + label
	used := prefixW + lipgloss.Width(rightText)
	fill := width - used
	if fill < 0 {
		fill = 0
	}
	line := styleFrame.Render("--") + labelRendered +
		styleFrame.Render(strings.Repeat(frameCharH, fill))
	if rightText != "" {
		line += " " + styleMeta.Render(right) + " " + styleFrame.Render(frameCharH)
	}
	return fitVisible(line, width)
}

// ruledHeader is kept as an alias so existing call sites compile unchanged.
func ruledHeader(label string, width int, right string, active bool) string {
	return sectionHeader(label, width, right, active)
}

// Frame chars — ASCII-safe, unambiguous single-width in all terminals.
// We avoid box-drawing Unicode (╭╮╰╯│─) because their east_asian_width is
// "Ambiguous" and some wcwidth tables (including lipgloss's) count them as 2.
const (
	frameCharTL  = "."   // top-left corner
	frameCharTR  = "."   // top-right corner
	frameCharBL  = "'"   // bottom-left corner
	frameCharBR  = "'"   // bottom-right corner
	frameCharH   = "-"   // horizontal
	frameCharV   = "|"   // vertical
	frameCharLT  = "+"   // left-T (mid-divider left)
	frameCharRT  = "+"   // right-T (mid-divider right)
	frameCharCRS = "+"   // cross / T-down
)

// frameSide renders a single "|" in frame color.
func frameSide() string {
	return styleFrame.Render(frameCharV)
}

// frameTop renders ".─ ◆ ROVE CODE ──────────────────────────────────────."
func frameTop(label string, width int) string {
	if width < 6 {
		return ""
	}
	mark := styleBrandMark.Render("◆")
	text := " " + mark + " " + styleBrand.Render(label) + " "
	textW := lipgloss.Width(text)
	// corners + "─" on each side: "." + "─" + text + fill×"─" + "."
	// visible: 1 + 1 + textW + fill + 1 = width  →  fill = width - textW - 3
	fill := width - textW - 3
	if fill < 0 {
		fill = 0
	}
	line := styleFrame.Render(frameCharTL+frameCharH) +
		text +
		styleFrame.Render(strings.Repeat(frameCharH, fill)+frameCharTR)
	return fitVisible(line, width)
}

// frameBottom renders "'────────────────────────────────────────────────────'"
func frameBottom(width int) string {
	if width < 2 {
		return ""
	}
	inner := width - 2
	return styleFrame.Render(frameCharBL + strings.Repeat(frameCharH, inner) + frameCharBR)
}

// frameDivider renders a mid-panel horizontal separator.
func frameDivider(width int) string {
	if width < 2 {
		return ""
	}
	inner := width - 2
	return styleFrame.Render(frameCharLT+strings.Repeat(frameCharH, inner)+frameCharRT)
}

// frameSplitDivider renders the divider between chat and right rail.
func frameSplitDivider(chatW, planW, totalW int) string {
	lFill := chatW
	rFill := planW
	if lFill < 0 {
		lFill = 0
	}
	if rFill < 0 {
		rFill = 0
	}
	_ = totalW
	return styleFrame.Render(frameCharLT +
		strings.Repeat(frameCharH, lFill) +
		frameCharCRS +
		strings.Repeat(frameCharH, rFill) +
		frameCharRT)
}

func fitVisible(s string, width int) string {
	if width <= 0 {
		return ""
	}
	plain := []rune(stripSimpleANSI(s))
	plainW := len(plain)

	var result string
	if plainW <= width {
		result = s + strings.Repeat(" ", width-plainW)
	} else {
		result = string(plain[:width])
	}

	// Secondary clamp: lipgloss.Width accounts for terminal wcwidth profile and may
	// see ambiguous chars (e.g. ◆, ─, │) as 2-wide. If so, strip ANSI and return plain text.
	if lipgloss.Width(result) > width {
		plain2 := []rune(stripSimpleANSI(result))
		if len(plain2) > width {
			plain2 = plain2[:width]
		}
		result = string(plain2) + strings.Repeat(" ", width-len(plain2))
	}
	return result
}

// stripSimpleANSI handles SGR sequences emitted by lipgloss.
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