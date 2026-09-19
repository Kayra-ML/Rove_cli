package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/sshtunnel"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// SSHPanel is a modal overlay for SSH host management (Ctrl+H to toggle).
type SSHPanel struct {
	hosts  []sshtunnel.SavedHost
	store  *sshtunnel.HostStore
	cursor int
	open   bool

	// add-host input state
	addMode  bool
	addAlias textinput.Model
	addSpec  textinput.Model
	addNote  textinput.Model
	addField int // 0=alias, 1=spec, 2=note

	width  int
	height int
	status string

	// active tunnel
	activeTunnel *sshtunnel.Tunnel
	activeAlias  string
}

func NewSSHPanel() *SSHPanel {
	alias := textinput.New()
	alias.Placeholder = "alias (e.g. vds1)"
	alias.CharLimit = 32

	spec := textinput.New()
	spec.Placeholder = "user@host:port"
	spec.CharLimit = 128

	note := textinput.New()
	note.Placeholder = "optional note"
	note.CharLimit = 64

	store, _ := sshtunnel.LoadHosts()
	if store == nil {
		store = &sshtunnel.HostStore{}
	}

	p := &SSHPanel{
		store:    store,
		addAlias: alias,
		addSpec:  spec,
		addNote:  note,
	}
	p.reloadHosts()
	return p
}

func (p *SSHPanel) reloadHosts() {
	if p.store != nil {
		p.hosts = append([]sshtunnel.SavedHost(nil), p.store.Hosts...)
	}
}

func (p *SSHPanel) IsOpen() bool  { return p.open }
func (p *SSHPanel) IsAddMode() bool { return p.addMode }

func (p *SSHPanel) Open() {
	p.open = true
	// Reload from disk each time panel opens
	store, err := sshtunnel.LoadHosts()
	if err == nil {
		p.store = store
		p.reloadHosts()
	}
}

func (p *SSHPanel) Close() {
	p.open = false
	p.addMode = false
	p.status = ""
}

func (p *SSHPanel) Toggle() {
	if p.open {
		p.Close()
	} else {
		p.Open()
	}
}

func (p *SSHPanel) MoveUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}

func (p *SSHPanel) MoveDown() {
	if p.cursor < len(p.hosts)-1 {
		p.cursor++
	}
}

func (p *SSHPanel) StartAdd() {
	p.addMode = true
	p.addField = 0
	p.addAlias.SetValue("")
	p.addSpec.SetValue("")
	p.addNote.SetValue("")
	p.addAlias.Focus()
	p.addSpec.Blur()
	p.addNote.Blur()
}

func (p *SSHPanel) CancelAdd() {
	p.addMode = false
	p.status = ""
}

// NextAddField advances to the next input field.
func (p *SSHPanel) NextAddField() bool {
	switch p.addField {
	case 0:
		p.addAlias.Blur()
		p.addSpec.Focus()
		p.addField = 1
	case 1:
		p.addSpec.Blur()
		p.addNote.Focus()
		p.addField = 2
	case 2:
		// Commit
		return p.CommitAdd()
	}
	return false
}

// CommitAdd saves the new host and exits add mode. Returns true on success.
func (p *SSHPanel) CommitAdd() bool {
	alias := strings.TrimSpace(p.addAlias.Value())
	spec := strings.TrimSpace(p.addSpec.Value())
	note := strings.TrimSpace(p.addNote.Value())
	if alias == "" || spec == "" {
		p.status = "alias and spec are required"
		return false
	}
	if err := p.store.Add(alias, spec, note); err != nil {
		p.status = "error: " + err.Error()
		return false
	}
	p.reloadHosts()
	p.addMode = false
	p.status = fmt.Sprintf("added: %s", alias)
	return true
}

// DeleteSelected removes the currently selected host.
func (p *SSHPanel) DeleteSelected() {
	if len(p.hosts) == 0 || p.cursor >= len(p.hosts) {
		return
	}
	h := p.hosts[p.cursor]
	if h.Alias == p.activeAlias {
		p.status = "disconnect before deleting active host"
		return
	}
	_ = p.store.Remove(h.Alias)
	p.reloadHosts()
	if p.cursor >= len(p.hosts) && p.cursor > 0 {
		p.cursor--
	}
	p.status = fmt.Sprintf("deleted: %s", h.Alias)
}

// SelectedHost returns the currently highlighted host, or nil.
func (p *SSHPanel) SelectedHost() *sshtunnel.SavedHost {
	if len(p.hosts) == 0 || p.cursor >= len(p.hosts) {
		return nil
	}
	return &p.hosts[p.cursor]
}

// ActiveTunnel returns the current tunnel (nil if local).
func (p *SSHPanel) ActiveTunnel() *sshtunnel.Tunnel { return p.activeTunnel }
func (p *SSHPanel) ActiveAlias() string             { return p.activeAlias }

// SetTunnel records the active tunnel and alias (set by model after Connect).
func (p *SSHPanel) SetTunnel(t *sshtunnel.Tunnel, alias string) {
	p.activeTunnel = t
	p.activeAlias = alias
	if t != nil {
		p.status = fmt.Sprintf("● Tunnel: %s (%s)", alias, t.LocalAddr())
		p.store.UpdateLastUsed(alias)
		p.reloadHosts()
	} else {
		p.status = "○ Local"
	}
}

// Disconnect closes the current tunnel.
func (p *SSHPanel) Disconnect() {
	if p.activeTunnel != nil {
		p.activeTunnel.Close()
		p.activeTunnel = nil
		p.activeAlias = ""
		p.status = "○ Local"
	}
}

// AddFieldUpdate forwards a key to the active text input and returns the updated value.
func (p *SSHPanel) AddFieldValue() string {
	switch p.addField {
	case 0:
		return p.addAlias.Value()
	case 1:
		return p.addSpec.Value()
	case 2:
		return p.addNote.Value()
	}
	return ""
}

// Render returns the 70-wide modal string.
func (p *SSHPanel) Render() string {
	const modalW = 70

	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("#00cccc")).Bold(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("#44cc44"))
	dim := styleDim
	sel := lipgloss.NewStyle().Background(lipgloss.Color("#003355")).Foreground(lipgloss.Color("#ffffff"))
	errStyle := styleError

	var rows []string

	// Title
	rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render(" 🔗 SSH Hosts"))
	rows = append(rows, dim.Render(strings.Repeat("─", modalW-4)))

	// Tunnel status line
	if p.activeTunnel != nil {
		rows = append(rows, green.Render(fmt.Sprintf("  ● Tunnel: %s (%s)", p.activeAlias, p.activeTunnel.LocalAddr())))
	} else {
		rows = append(rows, dim.Render("  ○ Local"))
	}
	rows = append(rows, "")

	if p.addMode {
		// Add-host form
		rows = append(rows, cyan.Render("  Add new host:"))
		rows = append(rows, "")

		fieldStyle := func(active bool) string {
			if active {
				return cyan.Render("▶ ")
			}
			return dim.Render("  ")
		}
		rows = append(rows, fieldStyle(p.addField == 0)+"Alias:  "+p.addAlias.View())
		rows = append(rows, fieldStyle(p.addField == 1)+"Host:   "+p.addSpec.View())
		rows = append(rows, fieldStyle(p.addField == 2)+"Note:   "+p.addNote.View())
		rows = append(rows, "")

		if p.status != "" {
			rows = append(rows, errStyle.Render("  "+p.status))
		}

		// Pad
		for len(rows) < 18 {
			rows = append(rows, "")
		}
		rows = append(rows, dim.Render("  Tab:next-field  Enter:save  Esc:cancel"))
	} else {
		// Host list
		if len(p.hosts) == 0 {
			rows = append(rows, dim.Render("  (no saved hosts — press 'a' to add one)"))
		}

		for i, h := range p.hosts {
			lastUsed := "never"
			if !h.LastUsed.IsZero() {
				lastUsed = h.LastUsed.Format("01-02 15:04")
			}
			note := ""
			if h.Note != "" {
				note = "  " + dim.Render(h.Note)
			}

			label := fmt.Sprintf("%-14s %-32s %s", h.Alias, h.Spec, lastUsed)
			if len(label) > modalW-6 {
				label = label[:modalW-7] + "…"
			}

			isActive := h.Alias == p.activeAlias
			line := label + note

			if isActive {
				line = green.Render(label) + note
			}

			if i == p.cursor {
				rows = append(rows, sel.Width(modalW-4).Render("  "+line))
			} else {
				rows = append(rows, "  "+line)
			}
		}

		if p.status != "" {
			rows = append(rows, "")
			rows = append(rows, dim.Render("  "+p.status))
		}

		// Pad
		for len(rows) < 18 {
			rows = append(rows, "")
		}
		rows = append(rows, dim.Render("  Enter:connect  a:add  d:delete  x:disconnect  Esc:close"))
	}

	content := strings.Join(rows, "\n")

	borderColor := lipgloss.Color("#00ccff")
	if p.activeTunnel != nil {
		borderColor = lipgloss.Color("#44cc44")
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(modalW).
		Padding(0, 1).
		Render(content)
}

// tunnelStatusText returns the status bar text for the current connection state.
func (p *SSHPanel) TunnelStatusText() string {
	if p.activeTunnel != nil {
		return fmt.Sprintf("● %s", p.activeAlias)
	}
	return ""
}

// FormatLastUsed formats a time for display.
func formatAge(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}