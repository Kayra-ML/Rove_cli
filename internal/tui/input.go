package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// SlashCommand represents a slash command option.
type SlashCommand struct {
	Name        string
	Description string
	Prompt      string
}

var defaultSlashCommands = []SlashCommand{
	{Name: "/help", Description: "Show help", Prompt: "Please explain what you can do."},
	{Name: "/clear", Description: "Clear history", Prompt: ""},
	{Name: "/diff", Description: "Show current diff", Prompt: "Show me the current git diff."},
	{Name: "/status", Description: "Git status", Prompt: "Show me the current git status."},
	{Name: "/plan", Description: "Make a plan", Prompt: "Please create a plan for the current task."},
	{Name: "/review", Description: "Code review", Prompt: "Please review the recent code changes."},
	{Name: "/cancel", Description: "Cancel current run", Prompt: ""},
}

// InputBar handles the bottom input area.
type InputBar struct {
	input         textinput.Model
	slashOpen     bool
	slashCursor   int
	slashFilter   string
	slashCommands []SlashCommand
	width         int
	height        int
	active        bool
	statusLine    string
}

func NewInputBar() *InputBar {
	ti := textinput.New()
	ti.Placeholder = "Type a message… (/ for commands)"
	ti.Focus()
	ti.CharLimit = 4096

	return &InputBar{
		input:         ti,
		slashCommands: defaultSlashCommands,
	}
}

func (b *InputBar) SetSize(w, h int) {
	b.width = w
	b.height = h
	b.input.Width = w - 4
}

func (b *InputBar) SetActive(active bool) {
	b.active = active
	if active {
		b.input.Focus()
	} else {
		b.input.Blur()
	}
}

func (b *InputBar) SetStatus(status string) {
	b.statusLine = status
}

func (b *InputBar) Value() string {
	return b.input.Value()
}

func (b *InputBar) Clear() {
	b.input.SetValue("")
	b.slashOpen = false
}

func (b *InputBar) IsSlashOpen() bool {
	return b.slashOpen
}

func (b *InputBar) OpenSlash() {
	b.slashOpen = true
	b.slashCursor = 0
	b.slashFilter = ""
}

func (b *InputBar) CloseSlash() {
	b.slashOpen = false
}

func (b *InputBar) SlashUp() {
	filtered := b.filteredCommands()
	if b.slashCursor > 0 {
		b.slashCursor--
	} else {
		b.slashCursor = len(filtered) - 1
	}
}

func (b *InputBar) SlashDown() {
	filtered := b.filteredCommands()
	if b.slashCursor < len(filtered)-1 {
		b.slashCursor++
	} else {
		b.slashCursor = 0
	}
}

func (b *InputBar) SlashSelect() (string, bool) {
	filtered := b.filteredCommands()
	if len(filtered) == 0 {
		return "", false
	}
	if b.slashCursor >= len(filtered) {
		b.slashCursor = 0
	}
	cmd := filtered[b.slashCursor]
	b.slashOpen = false
	b.input.SetValue(cmd.Prompt)
	return cmd.Name, true
}

func (b *InputBar) UpdateFilter(v string) {
	// Extract the slash word from current input
	if strings.HasPrefix(v, "/") {
		space := strings.Index(v, " ")
		if space == -1 {
			b.slashFilter = v[1:]
		} else {
			b.slashFilter = v[1:space]
		}
	} else {
		b.slashFilter = ""
	}
}

func (b *InputBar) filteredCommands() []SlashCommand {
	if b.slashFilter == "" {
		return b.slashCommands
	}
	var out []SlashCommand
	filter := strings.ToLower(b.slashFilter)
	for _, cmd := range b.slashCommands {
		if strings.Contains(strings.ToLower(cmd.Name), filter) ||
			strings.Contains(strings.ToLower(cmd.Description), filter) {
			out = append(out, cmd)
		}
	}
	return out
}

func (b *InputBar) Render() string {
	w := b.width - 2
	if w < 4 {
		w = 4
	}

	var sb strings.Builder

	// Status line
	statusStyle := styleDim
	if b.statusLine != "" {
		sb.WriteString(statusStyle.Width(w).Render(b.statusLine))
		sb.WriteString("\n")
	}

	// Input field
	promptPrefix := lipgloss.NewStyle().Foreground(colorUser).Bold(true).Render("❯ ")
	inputLine := promptPrefix + b.input.View()
	sb.WriteString(inputLine)

	// Slash command popup (rendered above the input)
	if b.slashOpen {
		popup := b.renderSlashPopup(w)
		// Prepend popup before the input
		full := popup + "\n" + sb.String()
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderActive).
			Width(w).
			Render(full)
	}

	border := panelStyle(b.active)
	return border.Width(w).Render(sb.String())
}

func (b *InputBar) renderSlashPopup(maxW int) string {
	filtered := b.filteredCommands()
	if len(filtered) == 0 {
		return styleDim.Render("  no commands matched")
	}

	var rows []string
	for i, cmd := range filtered {
		desc := cmd.Description
		nameW := 12
		line := cmd.Name
		if len(line) < nameW {
			line += strings.Repeat(" ", nameW-len(line))
		}
		line += "  " + desc
		if len(line) > maxW-4 {
			line = line[:maxW-5] + "…"
		}
		if i == b.slashCursor {
			rows = append(rows, styleSelected.Width(maxW-4).Render(line))
		} else {
			rows = append(rows, styleDim.Render(line))
		}
	}

	return strings.Join(rows, "\n")
}

func (b *InputBar) Model() *textinput.Model {
	return &b.input
}