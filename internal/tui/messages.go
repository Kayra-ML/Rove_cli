package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/types"
	"github.com/charmbracelet/lipgloss"
)

// DisplayMessage is the compact presentation model for chat history.
type DisplayMessage struct {
	Role      types.MessageRole
	Content   string
	ToolName  string
	ToolArgs  string
	ToolExtra string
	IsCard    bool
	CardID    string
	IsError   bool
	At        time.Time
}

type MessagesPanel struct {
	messages    []DisplayMessage
	scrollOff   int
	active      bool
	width       int
	height      int
	pendingCard *DisplayMessage
}

func NewMessagesPanel() *MessagesPanel { return &MessagesPanel{} }

func (p *MessagesPanel) SetSize(w, h int) {
	changed := p.width != w || p.height != h
	p.width, p.height = w, h
	if changed {
		p.scrollToBottom()
	}
}

func (p *MessagesPanel) SetActive(active bool) { p.active = active }

func (p *MessagesPanel) AddMessage(msg DisplayMessage) {
	p.messages = append(p.messages, msg)
	p.scrollToBottom()
}

func (p *MessagesPanel) AddUserMessage(content string) {
	p.AddMessage(DisplayMessage{
		Role:    types.RoleUser,
		Content: content,
		At:      time.Now(),
	})
}

func (p *MessagesPanel) AppendAssistantDelta(delta string) {
	if delta == "" {
		return
	}
	last := len(p.messages) - 1
	if last >= 0 && p.messages[last].Role == types.RoleAssistant && !p.messages[last].IsError {
		p.messages[last].Content += delta
		p.scrollToBottom()
		return
	}
	p.AddMessage(DisplayMessage{Role: types.RoleAssistant, Content: delta, At: time.Now()})
}

func (p *MessagesPanel) CompleteTool(name, extra string, isError bool) {
	for i := len(p.messages) - 1; i >= 0; i-- {
		if p.messages[i].Role == types.RoleTool && p.messages[i].ToolName == name {
			p.messages[i].ToolExtra = extra
			p.messages[i].IsError = isError
			p.scrollToBottom()
			return
		}
	}
	p.AddMessage(DisplayMessage{Role: types.RoleTool, ToolName: name, ToolExtra: extra, IsError: isError, At: time.Now()})
}

func (p *MessagesPanel) SetMessages(msgs []types.Message) {
	p.messages = p.messages[:0]
	for _, m := range msgs {
		dm := DisplayMessage{Role: m.Role, Content: m.Content, At: m.CreatedAt}
		if len(m.ToolCalls) > 0 {
			tc := m.ToolCalls[0]
			dm.ToolName, dm.ToolArgs = tc.Name, tc.ArgsJSON
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
	content := strings.TrimSpace(tr.Content)
	if content == "" {
		return "done"
	}
	lines := strings.Count(content, "\n") + 1
	if lines > 1 {
		return fmt.Sprintf("%d lines", lines)
	}
	if len([]rune(content)) > 42 {
		return truncate(content, 42)
	}
	return content
}

func (p *MessagesPanel) contentDimensions() (int, int) {
	innerW := p.width - 6
	if innerW < 12 {
		innerW = 12
	}
	contentH := p.height - 1
	if p.pendingCard != nil {
		contentH -= len(p.renderApprovalCardLines(innerW)) + 1
	}
	if contentH < 1 {
		contentH = 1
	}
	return innerW, contentH
}

func (p *MessagesPanel) scrollToBottom() {
	innerW, contentH := p.contentDimensions()
	total := len(p.renderedLines(innerW))
	p.scrollOff = total - contentH
	if p.scrollOff < 0 {
		p.scrollOff = 0
	}
}

func (p *MessagesPanel) ScrollUp() {
	if p.scrollOff > 0 {
		p.scrollOff--
	}
}

func (p *MessagesPanel) ScrollDown() {
	innerW, contentH := p.contentDimensions()
	maxOff := len(p.renderedLines(innerW)) - contentH
	if maxOff < 0 {
		maxOff = 0
	}
	if p.scrollOff < maxOff {
		p.scrollOff++
	}
}

func (p *MessagesPanel) HasPendingCard() bool { return p.pendingCard != nil }

func (p *MessagesPanel) SetPendingCard(card *DisplayMessage) {
	p.pendingCard = card
	p.scrollToBottom()
}

func (p *MessagesPanel) ClearPendingCard() {
	p.pendingCard = nil
	p.scrollToBottom()
}

func toolIcon(name string, isError bool) string {
	if isError {
		return styleError.Render("×")
	}
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "write"), strings.Contains(lower, "patch"), strings.Contains(lower, "edit"), strings.Contains(lower, "create"):
		return lipgloss.NewStyle().Foreground(colorYellow).Render("~")
	case strings.Contains(lower, "shell"), strings.Contains(lower, "bash"), strings.Contains(lower, "exec"), strings.Contains(lower, "command"):
		return lipgloss.NewStyle().Foreground(colorViolet).Render("$")
	default:
		return styleDim.Render("·")
	}
}

func diffStat(extra string) string {
	parts := strings.Fields(extra)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		switch {
		case strings.HasPrefix(part, "+"):
			out = append(out, styleSuccess.Render(part))
		case strings.HasPrefix(part, "-"):
			out = append(out, styleError.Render(part))
		default:
			out = append(out, styleMeta.Render(part))
		}
	}
	return strings.Join(out, " ")
}

func (p *MessagesPanel) renderMsg(msg DisplayMessage, innerW int) []string {
	var lines []string
	switch msg.Role {
	case types.RoleUser:
		lines = append(lines, lipgloss.NewStyle().Foreground(colorBlue).Bold(true).Render("you"))
		for _, line := range wrapText(strings.TrimSpace(msg.Content), innerW) {
			lines = append(lines, styleUserMsg.Render(line))
		}
		lines = append(lines, "")
	case types.RoleAssistant:
		lines = append(lines, styleSuccess.Bold(true).Render("rove"))
		for _, line := range wrapText(strings.TrimSpace(msg.Content), innerW) {
			lines = append(lines, styleAgentMsg.Render(line))
		}
		lines = append(lines, "")
	case types.RoleTool:
		name := msg.ToolName
		if name == "" {
			name = "tool"
		}
		action := shortenToolName(name)
		subject := toolSubject(msg)
		row := toolIcon(name, msg.IsError) + " " + styleMuted.Render(action)
		if subject != "" {
			row += " " + lipgloss.NewStyle().Foreground(colorWhite).Render(subject)
		}
		if msg.ToolExtra != "" && msg.ToolExtra != subject {
			isEdit := strings.Contains(name, "write") || strings.Contains(name, "patch") || strings.Contains(name, "edit")
			if isEdit {
				row += "  " + diffStat(msg.ToolExtra)
			} else {
				row += "  " + styleMeta.Render(msg.ToolExtra)
			}
		}
		lines = append(lines, truncateVisible(row, innerW))
	default:
		for _, line := range wrapText(msg.Content, innerW) {
			lines = append(lines, styleMuted.Render(line))
		}
	}
	return lines
}

func toolSubject(msg DisplayMessage) string {
	if msg.ToolArgs != "" {
		var args map[string]any
		if json.Unmarshal([]byte(msg.ToolArgs), &args) == nil {
			for _, key := range []string{"path", "file", "command", "query", "url"} {
				if raw, ok := args[key].(string); ok && raw != "" {
					if key == "path" || key == "file" {
						return shortDisplayPath(raw)
					}
					return truncate(raw, 42)
				}
			}
		}
	}
	if strings.Contains(msg.ToolExtra, "/") {
		return shortDisplayPath(msg.ToolExtra)
	}
	return ""
}

func shortDisplayPath(path string) string {
	clean := filepath.ToSlash(path)
	parts := strings.Split(clean, "/")
	if len(parts) <= 3 {
		return clean
	}
	return "…/" + strings.Join(parts[len(parts)-3:], "/")
}

func shortenToolName(name string) string {
	lower := strings.ToLower(name)
	replacer := strings.NewReplacer(
		"read_file", "read",
		"write_file", "write",
		"patch_file", "edit",
		"create_file", "create",
		"list_directory", "list",
		"search_files", "search",
		"run_shell", "shell",
		"run_command", "shell",
		"browser_exec", "browser",
		"bash", "shell",
	)
	return replacer.Replace(lower)
}

func (p *MessagesPanel) renderedLines(innerW int) []string {
	var all []string
	for _, msg := range p.messages {
		all = append(all, p.renderMsg(msg, innerW)...)
	}
	return all
}

func (p *MessagesPanel) Render() string {
	w := maxInt(p.width, 12)
	h := maxInt(p.height, 4)
	innerW, contentH := p.contentDimensions()
	header := sectionHeader("messages", w, fmt.Sprintf("%d", len(p.messages)), p.active)

	all := p.renderedLines(innerW)
	var visible []string
	if len(all) == 0 {
		visible = p.emptyState(innerW, contentH)
	} else {
		maxOff := len(all) - contentH
		if maxOff < 0 {
			maxOff = 0
		}
		p.scrollOff = clampInt(p.scrollOff, 0, maxOff)
		end := p.scrollOff + contentH
		if end > len(all) {
			end = len(all)
		}
		visible = append(visible, all[p.scrollOff:end]...)
		for len(visible) < contentH {
			visible = append(visible, "")
		}
	}

	rows := []string{header}
	for _, line := range visible {
		rows = append(rows, "  "+line)
	}
	if p.pendingCard != nil {
		rows = append(rows, "")
		for _, line := range p.renderApprovalCardLines(innerW) {
			rows = append(rows, "  "+line)
		}
	}
	for len(rows) < h {
		rows = append(rows, "")
	}
	if len(rows) > h {
		rows = rows[:h]
	}
	// Clamp each row to exact width w, then join — avoids lipgloss.Width(w) padding
	// misbehaving on ambiguous-width chars (◆, ─, │) in lipgloss v1.1.0.
	clamped := make([]string, len(rows))
	for i, r := range rows {
		clamped[i] = fitVisible(r, w)
	}
	// Do NOT use lipgloss.Render on the joined multiline string — lipgloss pads every
	// row to the longest row's width, which breaks our exact-width contract.
	return strings.Join(clamped, "\n")
}

func (p *MessagesPanel) emptyState(innerW, contentH int) []string {
	rows := make([]string, contentH)
	if contentH < 9 {
		return rows
	}
	// Pixel art logo — 5 rows
	logoLines := renderPixelLogo(innerW)
	subtitleText := "local-first coding agent"
	hintText := "/setup  --  ctrl+k  --  type a task"
	subtitle := styleMuted.Render(subtitleText)
	hint := styleDim.Render(hintText)

	start := contentH/2 - 4
	if start < 0 {
		start = 0
	}
	for i, l := range logoLines {
		if start+i < contentH {
			rows[start+i] = l
		}
	}
	if start+6 < contentH {
		rows[start+6] = centerVisible(subtitle, innerW)
	}
	if start+8 < contentH {
		rows[start+8] = centerVisible(hint, innerW)
	}
	return rows
}

func (p *MessagesPanel) renderApprovalCardLines(maxW int) []string {
	if maxW < 12 {
		maxW = 12
	}
	command := "waiting for approval"
	if p.pendingCard != nil && strings.TrimSpace(p.pendingCard.Content) != "" {
		command = strings.ReplaceAll(strings.TrimSpace(p.pendingCard.Content), "\n", " ")
	}
	accent := lipgloss.NewStyle().Foreground(colorYellow).Render("┃")
	title := styleHighlight.Bold(true).Render("permission required")
	body := lipgloss.NewStyle().Foreground(colorWhite).Render(truncate(command, maxW-3))
	actions := styleSuccess.Bold(true).Render("a allow") + styleMeta.Render("   ") + styleError.Render("d deny")
	return []string{
		accent + " " + title,
		accent + " " + body,
		accent + " " + actions,
	}
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
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		runes := []rune(paragraph)
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
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(runes[:n-1]) + "…"
}

func truncateVisible(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	return truncate(stripSimpleANSI(s), width)
}

func centerVisible(s string, width int) string {
	pad := (width - lipgloss.Width(s)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
