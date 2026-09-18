package tui

import (
	"fmt"
	"strings"

	"github.com/aether-dev/aether/internal/types"
	"github.com/charmbracelet/lipgloss"
)

// SessionPanel renders the session list.
type SessionPanel struct {
	sessions []types.Session
	cursor   int
	active   bool
	width    int
	height   int
}

func NewSessionPanel() *SessionPanel {
	return &SessionPanel{}
}

func (p *SessionPanel) SetSessions(sessions []types.Session) {
	p.sessions = sessions
	if p.cursor >= len(sessions) && len(sessions) > 0 {
		p.cursor = len(sessions) - 1
	}
}

func (p *SessionPanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *SessionPanel) SetActive(active bool) {
	p.active = active
}

func (p *SessionPanel) MoveUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}

func (p *SessionPanel) MoveDown() {
	if p.cursor < len(p.sessions)-1 {
		p.cursor++
	}
}

func (p *SessionPanel) Selected() *types.Session {
	if len(p.sessions) == 0 || p.cursor < 0 || p.cursor >= len(p.sessions) {
		return nil
	}
	return &p.sessions[p.cursor]
}

func (p *SessionPanel) Render() string {
	innerW := p.width - 4
	if innerW < 1 {
		innerW = 1
	}
	innerH := p.height - 2
	if innerH < 1 {
		innerH = 1
	}

	title := titleStyle(p.active).Render("Sessions")

	var rows []string
	rows = append(rows, title)

	if len(p.sessions) == 0 {
		rows = append(rows, styleDim.Render("  (none)"))
		rows = append(rows, styleDim.Render("  Ctrl+N: new"))
	}

	for i, sess := range p.sessions {
		label := sess.Title
		if label == "" {
			label = string(sess.ID)[:8]
		}
		if len(label) > innerW-2 {
			label = label[:innerW-2]
		}
		label = fmt.Sprintf(" %s", label)

		if i == p.cursor {
			rows = append(rows, styleSelected.Width(innerW).Render(label))
		} else {
			rows = append(rows, styleDim.Render(label))
		}

		if len(rows) >= innerH {
			break
		}
	}

	// Pad to fill height
	for len(rows) < innerH {
		rows = append(rows, "")
	}

	content := strings.Join(rows[:innerH], "\n")
	return panelStyle(p.active).
		Width(innerW).
		Height(innerH).
		Render(content)
}

// FileTreePanel renders a file tree.
type FileTreePanel struct {
	files  []string
	cursor int
	active bool
	width  int
	height int
}

func NewFileTreePanel() *FileTreePanel {
	return &FileTreePanel{}
}

func (p *FileTreePanel) SetFiles(files []string) {
	p.files = files
}

func (p *FileTreePanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *FileTreePanel) SetActive(active bool) {
	p.active = active
}

func (p *FileTreePanel) MoveUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}

func (p *FileTreePanel) MoveDown() {
	if p.cursor < len(p.files)-1 {
		p.cursor++
	}
}

func (p *FileTreePanel) Selected() string {
	if len(p.files) == 0 || p.cursor < 0 || p.cursor >= len(p.files) {
		return ""
	}
	return p.files[p.cursor]
}

func (p *FileTreePanel) Render() string {
	innerW := p.width - 4
	if innerW < 1 {
		innerW = 1
	}
	innerH := p.height - 2
	if innerH < 1 {
		innerH = 1
	}

	title := titleStyle(p.active).Render("Files")

	var rows []string
	rows = append(rows, title)

	if len(p.files) == 0 {
		rows = append(rows, styleDim.Render("  (empty)"))
	}

	// Calculate scroll offset to keep cursor visible
	scrollOff := 0
	maxVisible := innerH - 1
	if p.cursor >= maxVisible {
		scrollOff = p.cursor - maxVisible + 1
	}

	for i := scrollOff; i < len(p.files) && len(rows) < innerH; i++ {
		f := p.files[i]
		if len(f) > innerW-2 {
			f = "…" + f[len(f)-(innerW-3):]
		}

		// Indent directories
		depth := strings.Count(f, "/")
		if strings.HasSuffix(f, "/") {
			depth--
		}
		indent := strings.Repeat(" ", depth)

		// Icon
		icon := "·"
		if strings.HasSuffix(f, "/") {
			icon = "▸"
			f = strings.TrimSuffix(f, "/")
		}

		label := fmt.Sprintf("%s%s %s", indent, icon, f)
		if len(label) > innerW-1 {
			label = label[:innerW-1]
		}

		style := styleDim
		if i == p.cursor {
			style = lipgloss.NewStyle().Foreground(colorAgent)
		}
		rows = append(rows, style.Render(label))
	}

	for len(rows) < innerH {
		rows = append(rows, "")
	}

	content := strings.Join(rows[:innerH], "\n")
	return panelStyle(p.active).
		Width(innerW).
		Height(innerH).
		Render(content)
}