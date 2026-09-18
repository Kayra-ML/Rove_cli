package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CodePanel renders the current file being edited by the agent.
type CodePanel struct {
	filename     string
	lines        []string
	changedLines map[int]bool // 1-indexed line numbers that changed
	scrollOff    int
	active       bool
	width        int
	height       int
	diffMode     bool
	diffLines    []DiffLine
}

// DiffLine represents one line in a diff view.
type DiffLine struct {
	Kind    DiffKind // context, add, remove
	LineNum int
	Text    string
}

type DiffKind int

const (
	DiffContext DiffKind = iota
	DiffAdd
	DiffRemove
)

func NewCodePanel() *CodePanel {
	return &CodePanel{
		changedLines: make(map[int]bool),
	}
}

func (p *CodePanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *CodePanel) SetActive(active bool) {
	p.active = active
}

func (p *CodePanel) SetFile(filename string, content string) {
	p.filename = filename
	p.lines = strings.Split(content, "\n")
	p.changedLines = make(map[int]bool)
	p.diffMode = false
	p.scrollOff = 0
}

func (p *CodePanel) SetChangedLines(lineNums []int) {
	p.changedLines = make(map[int]bool)
	for _, n := range lineNums {
		p.changedLines[n] = true
	}
}

func (p *CodePanel) SetDiff(filename string, diff []DiffLine) {
	p.filename = filename
	p.diffLines = diff
	p.diffMode = true
	p.scrollOff = 0
}

func (p *CodePanel) ToggleDiff() {
	p.diffMode = !p.diffMode
}

func (p *CodePanel) ScrollUp() {
	if p.scrollOff > 0 {
		p.scrollOff--
	}
}

func (p *CodePanel) ScrollDown() {
	p.scrollOff++
}

func (p *CodePanel) Render() string {
	innerW := p.width - 4
	innerH := p.height - 2
	if innerW < 4 {
		innerW = 4
	}
	if innerH < 2 {
		innerH = 2
	}

	modeStr := "code"
	if p.diffMode {
		modeStr = "diff"
	}
	titleText := fmt.Sprintf("Code [%s]", modeStr)
	if p.filename != "" {
		short := p.filename
		if len(short) > innerW-12 {
			short = "…" + short[len(short)-(innerW-13):]
		}
		titleText = fmt.Sprintf("Code [%s] %s", modeStr, short)
	}
	title := titleStyle(p.active).Render(titleText)

	displayH := innerH - 1

	var renderedLines []string
	if p.diffMode {
		renderedLines = p.renderDiffLines(innerW)
	} else {
		renderedLines = p.renderCodeLines(innerW)
	}

	// Apply scroll
	off := p.scrollOff
	if off >= len(renderedLines) && len(renderedLines) > 0 {
		off = len(renderedLines) - 1
	}
	visible := renderedLines
	if off < len(renderedLines) {
		visible = renderedLines[off:]
	}
	if len(visible) > displayH {
		visible = visible[:displayH]
	}
	for len(visible) < displayH {
		visible = append(visible, "")
	}

	var sb strings.Builder
	sb.WriteString(title)
	sb.WriteString("\n")
	for i, line := range visible {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(line)
	}

	return panelStyle(p.active).
		Width(innerW).
		Height(innerH).
		Render(sb.String())
}

func (p *CodePanel) renderCodeLines(innerW int) []string {
	if len(p.lines) == 0 {
		return []string{styleDim.Render("  (no file loaded)")}
	}

	numWidth := len(fmt.Sprintf("%d", len(p.lines)))
	codeW := innerW - numWidth - 2
	if codeW < 4 {
		codeW = 4
	}

	var out []string
	for i, line := range p.lines {
		lineNum := i + 1
		numStr := fmt.Sprintf("%*d", numWidth, lineNum)

		// Truncate line
		if len(line) > codeW {
			line = line[:codeW-1] + "…"
		}

		row := numStr + " " + line

		if p.changedLines[lineNum] {
			out = append(out, styleHighlight.Render(row))
		} else {
			out = append(out, styleDim.Render(numStr)+" "+styleAgentMsg.Render(line))
		}
	}
	return out
}

func (p *CodePanel) renderDiffLines(innerW int) []string {
	if len(p.diffLines) == 0 {
		return []string{styleDim.Render("  (no diff)")}
	}

	var out []string
	for _, dl := range p.diffLines {
		text := dl.Text
		if len(text) > innerW-2 {
			text = text[:innerW-3] + "…"
		}
		switch dl.Kind {
		case DiffAdd:
			out = append(out, lipgloss.NewStyle().Foreground(colorSuccess).Render("+ "+text))
		case DiffRemove:
			out = append(out, lipgloss.NewStyle().Foreground(colorError).Render("- "+text))
		default:
			out = append(out, styleDim.Render("  "+text))
		}
	}
	return out
}

// ParseUnifiedDiff converts a unified diff string into DiffLine slices.
func ParseUnifiedDiff(raw string) []DiffLine {
	var out []DiffLine
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			out = append(out, DiffLine{Kind: DiffAdd, Text: line[1:]})
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			out = append(out, DiffLine{Kind: DiffRemove, Text: line[1:]})
		} else if strings.HasPrefix(line, "@@") {
			out = append(out, DiffLine{Kind: DiffContext, Text: line})
		} else {
			text := line
			if len(text) > 0 && text[0] == ' ' {
				text = text[1:]
			}
			out = append(out, DiffLine{Kind: DiffContext, Text: text})
		}
	}
	return out
}