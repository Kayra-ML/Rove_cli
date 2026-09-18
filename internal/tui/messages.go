package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aether-dev/aether/internal/types"
	"github.com/charmbracelet/lipgloss"
)

// DisplayMessage is a rendered message entry in the messages panel.
type DisplayMessage struct {
	Role      types.MessageRole
	Content   string
	ToolName  string
	ToolArgs  string
	ToolExtra string // e.g. "+3 -1" for edits, "42 lines" for reads
	IsCard    bool   // approval card
	CardID    string
	IsError   bool
	At        time.Time
}

// MessagesPanel renders the chat + tool history.
type MessagesPanel struct {
	messages    []DisplayMessage
	scrollOff   int
	active      bool
	width       int
	height      int
	pendingCard *DisplayMessage // card awaiting approval
}

func NewMessagesPanel() *MessagesPanel {
	return &MessagesPanel{}
}

func (p *MessagesPanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *MessagesPanel) SetActive(active bool) {
	p.active = active
}

func (p *MessagesPanel) AddMessage(msg DisplayMessage) {
	p.messages = append(p.messages, msg)
	// Auto-scroll to bottom
	p.scrollToBottom()
}

func (p *MessagesPanel) SetMessages(msgs []types.Message) {
	p.messages = nil
	for _, m := range msgs {
		dm := DisplayMessage{
			Role:    m.Role,
			Content: m.Content,
			At:      m.CreatedAt,
		}
		if len(m.ToolCalls) > 0 {
			tc := m.ToolCalls[0]
			dm.ToolName = tc.Name
			dm.ToolArgs = tc.ArgsJSON
		}
		if m.ToolResult != nil {
			dm.Role = types.RoleTool
			dm.ToolName = m.ToolResult.Name
			dm.IsError = m.ToolResult.IsError
			dm.ToolExtra = summarizeToolResult(m.ToolResult)
		}
		p.messages = append(p.messages, dm)
	}
	p.scrollToBottom()
}

func summarizeToolResult(tr *types.ToolResult) string {
	if tr == nil {
		return ""
	}
	content := tr.Content
	lines := strings.Count(content, "\n") + 1
	if lines > 1 {
		return fmt.Sprintf("%d lines", lines)
	}
	if len(content) > 40 {
		return content[:37] + "..."
	}
	return content
}

func (p *MessagesPanel) scrollToBottom() {
	inner := p.height - 4
	if inner <= 0 {
		inner = 1
	}
	total := p.countRenderedLines()
	if total > inner {
		p.scrollOff = total - inner
	} else {
		p.scrollOff = 0
	}
}

func (p *MessagesPanel) ScrollUp() {
	if p.scrollOff > 0 {
		p.scrollOff--
	}
}

func (p *MessagesPanel) ScrollDown() {
	p.scrollOff++
}

func (p *MessagesPanel) HasPendingCard() bool {
	return p.pendingCard != nil
}

func (p *MessagesPanel) SetPendingCard(card *DisplayMessage) {
	p.pendingCard = card
}

func (p *MessagesPanel) ClearPendingCard() {
	p.pendingCard = nil
}

func (p *MessagesPanel) countRenderedLines() int {
	count := 0
	innerW := p.width - 4
	if innerW < 10 {
		innerW = 10
	}
	for _, msg := range p.messages {
		lines := p.renderMsg(msg, innerW)
		count += len(lines)
	}
	return count
}

func (p *MessagesPanel) renderMsg(msg DisplayMessage, innerW int) []string {
	var lines []string
	switch msg.Role {
	case types.RoleUser:
		prefix := "▶ "
		text := wrapText(msg.Content, innerW-2)
		for i, line := range text {
			if i == 0 {
				lines = append(lines, styleUserMsg.Render(prefix+line))
			} else {
				lines = append(lines, styleUserMsg.Render("  "+line))
			}
		}
	case types.RoleAssistant:
		text := wrapText(msg.Content, innerW)
		for _, line := range text {
			lines = append(lines, styleAgentMsg.Render(line))
		}
	case types.RoleTool:
		name := msg.ToolName
		if name == "" {
			name = "tool"
		}
		extra := msg.ToolExtra
		errMark := ""
		if msg.IsError {
			errMark = " " + styleError.Render("✗")
		}
		row := fmt.Sprintf("  ⚙ %s", name)
		if extra != "" {
			row += " · " + extra
		}
		row += errMark
		lines = append(lines, styleToolRow.Render(row))
	default:
		if msg.Content != "" {
			text := wrapText(msg.Content, innerW)
			for _, line := range text {
				lines = append(lines, styleDim.Render(line))
			}
		}
	}
	return lines
}

func (p *MessagesPanel) Render() string {
	innerW := p.width - 4
	innerH := p.height - 2
	if innerW < 4 {
		innerW = 4
	}
	if innerH < 2 {
		innerH = 2
	}

	title := titleStyle(p.active).Render("Messages")

	// Collect all rendered lines
	var allLines []string
	for _, msg := range p.messages {
		allLines = append(allLines, p.renderMsg(msg, innerW)...)
	}

	// Apply scroll
	off := p.scrollOff
	if off > len(allLines) {
		off = len(allLines)
	}
	visible := allLines[off:]

	// Limit to innerH - 1 (leave 1 row for title)
	displayH := innerH - 1
	if len(visible) > displayH {
		visible = visible[len(visible)-displayH:]
	}

	// Pad
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
		// Truncate to width
		if utf8.RuneCountInString(line) > innerW {
			runes := []rune(line)
			line = string(runes[:innerW])
		}
		sb.WriteString(line)
	}

	content := sb.String()

	// Overlay approval card if pending
	if p.pendingCard != nil {
		card := p.renderApprovalCard(p.pendingCard, innerW)
		// Place card at center of panel
		content = overlayCard(content, card, innerH, innerW)
	}

	return panelStyle(p.active).
		Width(innerW).
		Height(innerH).
		Render(content)
}

func (p *MessagesPanel) renderApprovalCard(card *DisplayMessage, maxW int) string {
	title := styleApprovalBorder.Render(
		lipgloss.NewStyle().Foreground(colorApproval).Bold(true).Render("⚠ Approval Required") + "\n" +
			styleAgentMsg.Render(truncate(card.Content, maxW-6)) + "\n" +
			"\n" +
			lipgloss.NewStyle().Foreground(colorSuccess).Render("[A]pprove") +
			"  " +
			lipgloss.NewStyle().Foreground(colorError).Render("[D]eny"),
	)
	return title
}

func overlayCard(bg, card string, h, w int) string {
	_ = h
	_ = w
	// Simple: put card below existing content
	return bg + "\n" + card
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		width = 40
	}
	var lines []string
	paragraphs := strings.Split(text, "\n")
	for _, para := range paragraphs {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		runes := []rune(para)
		for len(runes) > width {
			// Try to break at a space
			breakAt := width
			for i := width; i > width/2; i-- {
				if runes[i] == ' ' {
					breakAt = i
					break
				}
			}
			lines = append(lines, string(runes[:breakAt]))
			runes = runes[breakAt:]
			// Trim leading space
			for len(runes) > 0 && runes[0] == ' ' {
				runes = runes[1:]
			}
		}
		if len(runes) > 0 {
			lines = append(lines, string(runes))
		}
	}
	return lines
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}