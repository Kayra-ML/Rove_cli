package tui

import (
	"fmt"
	"strings"
	"time"

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
	inner := p.height - 3
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
	innerW := p.width - 2
	if innerW < 10 {
		innerW = 10
	}
	for _, msg := range p.messages {
		lines := p.renderMsg(msg, innerW)
		count += len(lines)
	}
	return count
}

// toolIcon returns the leading character for a tool row.
// Read/list → dot, edit/write/patch → tilde, shell → dollar, error → cross.
func toolIcon(name string, isError bool) string {
	if isError {
		return styleError.Render("✗")
	}
	switch {
	case strings.Contains(name, "write") ||
		strings.Contains(name, "patch") ||
		strings.Contains(name, "edit") ||
		strings.Contains(name, "create"):
		return styleDim.Render("~")
	case strings.Contains(name, "shell") ||
		strings.Contains(name, "bash") ||
		strings.Contains(name, "exec"):
		return styleDim.Render("$")
	default:
		return styleDim.Render("·")
	}
}

// diffStat formats "+N -M" with green/red coloring inline.
func diffStat(extra string) string {
	// If it already contains + and -, color it
	parts := strings.Fields(extra)
	var out []string
	for _, p := range parts {
		switch {
		case strings.HasPrefix(p, "+"):
			out = append(out, lipgloss.NewStyle().Foreground(colorGreen).Render(p))
		case strings.HasPrefix(p, "-"):
			out = append(out, lipgloss.NewStyle().Foreground(colorRed).Render(p))
		default:
			out = append(out, styleDim.Render(p))
		}
	}
	if len(out) == 0 {
		return styleDim.Render(extra)
	}
	return strings.Join(out, " ")
}

func (p *MessagesPanel) renderMsg(msg DisplayMessage, innerW int) []string {
	var lines []string
	switch msg.Role {
	case types.RoleUser:
		// Bold white, no prefix icon clutter — clean user line
		wrapped := wrapText(msg.Content, innerW)
		for i, line := range wrapped {
			if i == 0 {
				lines = append(lines, styleUserMsg.Render(line))
			} else {
				lines = append(lines, styleUserMsg.Render(line))
			}
		}
		// Blank spacer after user message
		lines = append(lines, "")

	case types.RoleAssistant:
		wrapped := wrapText(msg.Content, innerW)
		for _, line := range wrapped {
			lines = append(lines, styleAgentMsg.Render(line))
		}
		// Blank spacer after agent block
		if len(wrapped) > 0 {
			lines = append(lines, "")
		}

	case types.RoleTool:
		name := msg.ToolName
		if name == "" {
			name = "tool"
		}
		extra := msg.ToolExtra
		icon := toolIcon(name, msg.IsError)

		// Compact single line: "· read_file  callback.ts  16 lines"
		//                 or:  "~ patch_file  callback.ts  +4 -1"
		shortName := shortenToolName(name)
		row := icon + " " + styleDim.Render(shortName)
		if extra != "" {
			isEdit := strings.Contains(name, "write") ||
				strings.Contains(name, "patch") ||
				strings.Contains(name, "edit")
			if isEdit {
				row += "  " + diffStat(extra)
			} else {
				row += "  " + styleDim.Render(extra)
			}
		}
		lines = append(lines, row)

	default:
		if msg.Content != "" {
			wrapped := wrapText(msg.Content, innerW)
			for _, line := range wrapped {
				lines = append(lines, styleDim.Render(line))
			}
		}
	}
	return lines
}

// shortenToolName makes tool names shorter for compact display.
func shortenToolName(name string) string {
	replacer := strings.NewReplacer(
		"read_file", "read",
		"write_file", "write",
		"patch_file", "patch",
		"list_directory", "ls",
		"run_shell", "shell",
		"bash", "shell",
	)
	return replacer.Replace(name)
}

func (p *MessagesPanel) Render() string {
	w := p.width
	if w < 6 {
		w = 6
	}
	innerW := w - 2
	if innerW < 4 {
		innerW = 4
	}
	h := p.height
	if h < 4 {
		h = 4
	}

	// Header: thin separator line + label
	sep := styleSep.Render(strings.Repeat("─", w))
	header := styleDim.Render("  messages")
	headerLines := 2 // sep + label

	contentH := h - headerLines

	// Approval card overlay (rendered at bottom of message area)
	var cardLines []string
	if p.pendingCard != nil {
		cardLines = p.renderApprovalCardLines(innerW)
		contentH -= len(cardLines) + 1 // +1 for spacer
	}

	if contentH < 1 {
		contentH = 1
	}

	// Collect all rendered message lines
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

	// Trim to contentH (show most recent)
	if len(visible) > contentH {
		visible = visible[len(visible)-contentH:]
	}

	// Pad
	for len(visible) < contentH {
		visible = append(visible, "")
	}

	var sb strings.Builder
	sb.WriteString(sep)
	sb.WriteByte('\n')
	sb.WriteString(header)
	sb.WriteByte('\n')

	for i, line := range visible {
		if i > 0 {
			sb.WriteByte('\n')
		}
		// Left-pad content by 2 for clean alignment
		sb.WriteString("  ")
		sb.WriteString(line)
	}

	// Append approval card at bottom
	if len(cardLines) > 0 {
		sb.WriteByte('\n')
		for _, cl := range cardLines {
			sb.WriteByte('\n')
			sb.WriteString(cl)
		}
	}

	return lipgloss.NewStyle().
		Width(w).
		Height(h).
		Background(colorBg).
		Render(sb.String())
}

func (p *MessagesPanel) renderApprovalCardLines(maxW int) []string {
	var lines []string

	// Highlighted yellow row for approval card
	cardW := maxW
	if cardW > maxW {
		cardW = maxW
	}

	title := truncate("  ◆ needs your permission", cardW)
	content := ""
	if p.pendingCard != nil {
		content = truncate("  "+p.pendingCard.Content, cardW)
	}
	buttons := "  " +
		lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render("[a] allow") +
		"  " +
		lipgloss.NewStyle().Foreground(colorRed).Render("[d] deny")

	lines = append(lines,
		styleApprovalRow.Width(maxW).Render(title),
		styleApprovalRow.Width(maxW).Render(content),
		buttons,
	)
	return lines
}

func overlayCard(bg, card string, h, w int) string {
	_ = h
	_ = w
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
			breakAt := width
			for i := width; i > width/2; i-- {
				if runes[i] == ' ' {
					breakAt = i
					break
				}
			}
			lines = append(lines, string(runes[:breakAt]))
			runes = runes[breakAt:]
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