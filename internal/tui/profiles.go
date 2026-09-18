package tui

import (
	"fmt"
	"strings"

	"github.com/aether-dev/aether/internal/types"
	"github.com/charmbracelet/lipgloss"
)

// roleEmoji returns the emoji for a given agent role.
func roleEmoji(role types.AgentRole) string {
	switch role {
	case types.RoleLeader:
		return "👑"
	case types.RoleFrontend:
		return "🎨"
	case types.RoleBackend:
		return "⚙️"
	case types.RoleDeveloper:
		return "💻"
	case types.RoleDesigner:
		return "✏️"
	case types.RoleTester:
		return "🧪"
	case types.RoleDebugger:
		return "🔍"
	case types.RoleReviewer:
		return "🔎"
	case types.RoleResearcher:
		return "📚"
	default:
		return "🤖"
	}
}

// shortModel returns a shortened model name for display.
func shortModel(model string) string {
	if model == "" {
		return ""
	}
	// Strip common prefixes
	for _, prefix := range []string{"anthropic/", "openai/", "google/"} {
		model = strings.TrimPrefix(model, prefix)
	}
	// Truncate if too long
	if len(model) > 16 {
		return model[:15] + "…"
	}
	return model
}

// ProfilePanel renders a profile list panel.
type ProfilePanel struct {
	profiles      []types.AgentProfile
	active        string // current profile ID (active/default)
	cursor        int
	open          bool // modal open
	width         int
	height        int
	newNameInput  string
	enteringName  bool // inline new-profile prompt
}

func NewProfilePanel() *ProfilePanel {
	return &ProfilePanel{}
}

func (p *ProfilePanel) SetProfiles(profiles []types.AgentProfile) {
	p.profiles = profiles
	// Detect active/default profile
	for _, pr := range profiles {
		if pr.IsDefault {
			p.active = string(pr.ID)
			break
		}
	}
	if p.cursor >= len(profiles) && len(profiles) > 0 {
		p.cursor = len(profiles) - 1
	}
}

func (p *ProfilePanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *ProfilePanel) Open() {
	p.open = true
}

func (p *ProfilePanel) Close() {
	p.open = false
	p.enteringName = false
	p.newNameInput = ""
}

func (p *ProfilePanel) Toggle() {
	if p.open {
		p.Close()
	} else {
		p.Open()
	}
}

func (p *ProfilePanel) IsOpen() bool {
	return p.open
}

func (p *ProfilePanel) MoveUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}

func (p *ProfilePanel) MoveDown() {
	if p.cursor < len(p.profiles)-1 {
		p.cursor++
	}
}

func (p *ProfilePanel) Selected() *types.AgentProfile {
	if len(p.profiles) == 0 || p.cursor < 0 || p.cursor >= len(p.profiles) {
		return nil
	}
	return &p.profiles[p.cursor]
}

// SelectedID returns the ID of the selected profile.
func (p *ProfilePanel) SelectedID() string {
	if sel := p.Selected(); sel != nil {
		return string(sel.ID)
	}
	return ""
}

// ActiveProfileName returns the name of the active (default) profile.
func (p *ProfilePanel) ActiveProfileName() string {
	for _, pr := range p.profiles {
		if string(pr.ID) == p.active {
			return pr.Name
		}
	}
	if len(p.profiles) > 0 {
		return p.profiles[0].Name
	}
	return ""
}

// IsEnteringName returns true when the inline new-profile prompt is open.
func (p *ProfilePanel) IsEnteringName() bool {
	return p.enteringName
}

// StartNewProfile opens the inline new-profile input.
func (p *ProfilePanel) StartNewProfile() {
	p.enteringName = true
	p.newNameInput = ""
}

// AppendNameChar appends a character to the new-profile name input.
func (p *ProfilePanel) AppendNameChar(ch rune) {
	p.newNameInput += string(ch)
}

// BackspaceName removes the last character from the new-profile name input.
func (p *ProfilePanel) BackspaceName() {
	if len(p.newNameInput) > 0 {
		p.newNameInput = p.newNameInput[:len(p.newNameInput)-1]
	}
}

// CommitNewProfile finalises the new profile name entry and returns it.
func (p *ProfilePanel) CommitNewProfile() string {
	name := strings.TrimSpace(p.newNameInput)
	p.enteringName = false
	p.newNameInput = ""
	return name
}

// CancelNewProfile cancels the inline new-profile input.
func (p *ProfilePanel) CancelNewProfile() {
	p.enteringName = false
	p.newNameInput = ""
}

// Render draws the profile panel content (for use inside a modal overlay).
func (p *ProfilePanel) Render() string {
	modalW := 60
	modalH := 20

	dimGreen := lipgloss.NewStyle().Foreground(lipgloss.Color("#44aa44")).Faint(true)
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("#00cccc")).Bold(true)
	selected := lipgloss.NewStyle().Background(lipgloss.Color("#003355")).Foreground(lipgloss.Color("#ffffff"))
	dim := styleDim

	title := fmt.Sprintf(" Profiles (%d)", len(p.profiles))
	titleRendered := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render(title)

	var rows []string
	rows = append(rows, titleRendered)
	rows = append(rows, styleDim.Render(strings.Repeat("─", modalW-4)))

	if len(p.profiles) == 0 {
		rows = append(rows, dim.Render("  (no profiles — press 'n' to create one)"))
	}

	for i, pr := range p.profiles {
		emoji := roleEmoji(pr.Role)
		modelStr := shortModel(pr.Model)
		label := fmt.Sprintf("%s %-20s %s", emoji, pr.Name, modelStr)
		if len(label) > modalW-6 {
			label = label[:modalW-7] + "…"
		}

		isActive := string(pr.ID) == p.active
		isDefault := pr.IsDefault

		if isActive {
			label = cyan.Render(label)
		}
		if isDefault {
			label += " " + dimGreen.Render("✓")
		}

		if i == p.cursor {
			row := selected.Width(modalW - 4).Render("  " + label)
			rows = append(rows, row)
		} else {
			rows = append(rows, "  "+label)
		}
	}

	// Inline new-profile prompt
	if p.enteringName {
		rows = append(rows, "")
		rows = append(rows, dim.Render("  New profile name: ")+p.newNameInput+"█")
	}

	// Pad to fill height
	for len(rows) < modalH-4 {
		rows = append(rows, "")
	}

	// Help line at bottom
	helpLine := dim.Render("  ↑↓:nav  Enter:set-default  n:new  d:delete  Ctrl+P:close")
	rows = append(rows, helpLine)

	content := strings.Join(rows, "\n")

	// Border the modal
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#00ccff")).
		Width(modalW).
		Height(modalH).
		Padding(0, 1).
		Render(content)
}
