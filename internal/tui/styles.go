package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Rove Code — Navy palette
// Deep navy background, bright white accents, violet brand, warm execution.
// ---------------------------------------------------------------------------
var (
	// backgrounds — layered navy depth
	colorBg       = lipgloss.Color("#0A0E1A") // deepest navy
	colorBgPanel  = lipgloss.Color("#0D1220") // panel surface
	colorSelected = lipgloss.Color("#1A2236")

	// text scale
	colorWhite = lipgloss.Color("#EFF1F8") // primary text — near-white
	colorMid   = lipgloss.Color("#8B92AA") // secondary text
	colorDim   = lipgloss.Color("#3D4560") // muted / disabled

	// frame — bright white, solid, never dashed
	colorFrame = lipgloss.Color("#FFFFFF")

	// semantic
	colorGreen  = lipgloss.Color("#5DD88A")
	colorYellow = lipgloss.Color("#E8C468")
	colorRed    = lipgloss.Color("#E56B72")
	colorBlue   = lipgloss.Color("#6EA8FC")
	colorViolet = lipgloss.Color("#B49AFA") // brand accent — brighter

	// Compatibility aliases
	colorAgent   = colorWhite
	colorError   = colorRed
	colorSuccess = colorGreen

	// -----------------------------------------------------------------------
	// Styles
	// -----------------------------------------------------------------------
	stylePanelBorder = lipgloss.NewStyle().Background(colorBg)

	styleTitle       = lipgloss.NewStyle().Foreground(colorMid)
	styleTitleActive = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	styleBrand       = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	styleBrandMark   = lipgloss.NewStyle().Foreground(colorViolet).Bold(true)
	styleMeta        = lipgloss.NewStyle().Foreground(colorDim)

	styleUserMsg  = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	styleAgentMsg = lipgloss.NewStyle().Foreground(colorWhite)

	styleError     = lipgloss.NewStyle().Foreground(colorRed)
	styleSuccess   = lipgloss.NewStyle().Foreground(colorGreen)
	styleDim       = lipgloss.NewStyle().Foreground(colorDim)
	styleMuted     = lipgloss.NewStyle().Foreground(colorMid)
	styleHighlight = lipgloss.NewStyle().Foreground(colorYellow)
	styleSelected  = lipgloss.NewStyle().Background(colorSelected).Foreground(colorWhite)
	styleFrame     = lipgloss.NewStyle().Foreground(colorFrame).Bold(true)
	styleStatusBar = lipgloss.NewStyle().Foreground(colorMid).Background(colorBgPanel)

	// Plan item state icons
	iconDone    = styleSuccess.Render("#")
	iconCurrent = styleHighlight.Bold(true).Render("*")
	iconPending = styleDim.Render("o")
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

// ---------------------------------------------------------------------------
// Section headers  -- label -------------------- meta -
// ---------------------------------------------------------------------------
func sectionHeader(label string, width int, right string, active bool) string {
	if width <= 0 {
		return ""
	}
	labelRendered := " " + titleStyle(active).Render(label) + " "
	rightText := ""
	if right != "" {
		rightText = " " + styleMeta.Render(right) + " " + styleFrame.Render(frameCharH)
	}
	prefixW := 2 + lipgloss.Width(labelRendered)
	used := prefixW + lipgloss.Width(rightText)
	fill := width - used
	if fill < 0 {
		fill = 0
	}
	line := styleFrame.Render(strings.Repeat(frameCharH, 2)) + labelRendered +
		styleFrame.Render(strings.Repeat(frameCharH, fill))
	if rightText != "" {
		line += " " + styleMeta.Render(right) + " " + styleFrame.Render(frameCharH)
	}
	return fitVisible(line, width)
}

func ruledHeader(label string, width int, right string, active bool) string {
	return sectionHeader(label, width, right, active)
}

// ---------------------------------------------------------------------------
// Frame characters — all ASCII (single-width, no ambiguous wcwidth)
// ---------------------------------------------------------------------------
const (
	frameCharTL  = "+"
	frameCharTR  = "+"
	frameCharBL  = "+"
	frameCharBR  = "+"
	frameCharH   = "=" // kalın, kesiksiz yatay çizgi
	frameCharV   = "|"
	frameCharLT  = "+"
	frameCharRT  = "+"
	frameCharCRS = "+"
)

func frameSide() string {
	return styleFrame.Render(frameCharV)
}

// frameBottom: "+============================================+"
func frameBottom(width int) string {
	if width < 2 {
		return ""
	}
	inner := width - 2
	return fitVisible(styleFrame.Render(frameCharBL+strings.Repeat(frameCharH, inner)+frameCharBR), width)
}

// frameSplitDivider is unused — renderMidDivider builds inline.

// ---------------------------------------------------------------------------
// Width helpers — rune-count primary, lipgloss secondary clamp
// ---------------------------------------------------------------------------

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

	// Secondary clamp: lipgloss.Width varies by terminal wcwidth profile.
	if lipgloss.Width(result) > width {
		plain2 := []rune(stripSimpleANSI(result))
		if len(plain2) > width {
			plain2 = plain2[:width]
		}
		result = string(plain2) + strings.Repeat(" ", width-len(plain2))
	}
	return result
}

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

// ---------------------------------------------------------------------------
// Figlet ROVE wordmark (user-supplied). 6 rows × 37 cols. Box-drawing is
// single-width in lipgloss v1.1.0 — still clamped per-row in emptyState.
// ---------------------------------------------------------------------------
var pixelLogoLines = []string{
	"██████╗   ██████╗  ██╗   ██╗ ███████╗",
	"██╔══██╗ ██╔═══██╗ ██║   ██║ ██╔════╝",
	"██████╔╝ ██║   ██║ ██║   ██║ █████╗  ",
	"██╔══██╗ ██║   ██║ ╚██╗ ██╔╝ ██╔══╝  ",
	"██║  ██║ ╚██████╔╝  ╚████╔╝  ███████╗",
	"╚═╝  ╚═╝  ╚═════╝    ╚═══╝   ╚══════╝",
}

func renderPixelLogo(w int) []string {
	logoW := 0
	for _, row := range pixelLogoLines {
		if n := len([]rune(row)); n > logoW {
			logoW = n
		}
	}
	pad := (w - logoW) / 2
	if pad < 0 {
		pad = 0
	}
	prefix := strings.Repeat(" ", pad)
	lines := make([]string, len(pixelLogoLines))
	for i, row := range pixelLogoLines {
		rendered := ""
		for _, ch := range row {
			if ch == ' ' {
				rendered += " "
				continue
			}
			rendered += styleBrand.Render(string(ch))
		}
		lines[i] = prefix + rendered
	}
	return lines
}
