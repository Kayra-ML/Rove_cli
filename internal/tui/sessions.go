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
		indent := strings.Repeat("  ", depth)

		// Icon by type / extension
		icon := fileIcon(f)

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
// fileIcon returns a Nerd Font glyph for a file or directory.
// Requires a Nerd Font in the terminal (JetBrainsMono NF, FiraCode NF, etc.)
func fileIcon(f string) string {
	// Directories
	if strings.HasSuffix(f, "/") {
		return "\uf07b" // nf-fa-folder
	}

	// Special filenames first
	base := f
	if idx := strings.LastIndex(f, "/"); idx != -1 {
		base = f[idx+1:]
	}
	switch strings.ToLower(base) {
	case "dockerfile", ".dockerfile":
		return "\uf308" // nf-linux-docker
	case "makefile", "gnumakefile":
		return "\uf0ad" // nf-fa-wrench
	case "readme", "readme.md", "readme.txt", "readme.rst":
		return "\uf48a" // nf-oct-book
	case "license", "licence", "license.md", "licence.md":
		return "\uf22d" // nf-fa-balance_scale
	case ".gitignore", ".gitattributes", ".gitmodules":
		return "\ue702" // nf-dev-git
	case "package.json":
		return "\ue60c" // nf-dev-nodejs_small
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lockb":
		return "\uf023" // nf-fa-lock
	case "go.mod", "go.sum":
		return "\ue627" // nf-dev-go
	case "cargo.toml", "cargo.lock":
		return "\ue7a8" // nf-dev-rust
	case "tsconfig.json", "tsconfig.node.json":
		return "\ue628" // nf-dev-typescript
	case ".eslintrc", ".eslintrc.js", ".eslintrc.json", ".eslintrc.yaml":
		return "\ue655" // nf-seti-eslint
	case ".prettierrc", ".prettierrc.json", ".prettierrc.js":
		return "\ue6b4" // nf-seti-prettier
	case "dockerfile.dev", "docker-compose.yml", "docker-compose.yaml":
		return "\uf308" // nf-linux-docker
	case ".env", ".env.local", ".env.production", ".env.development":
		return "\uf013" // nf-fa-cog
	}

	// Extension map
	ext := ""
	if idx := strings.LastIndex(f, "."); idx != -1 {
		ext = strings.ToLower(f[idx:])
	}

	switch ext {
	// Go
	case ".go":
		return "\ue627" // nf-dev-go
	// TypeScript
	case ".ts":
		return "\ue628" // nf-dev-typescript
	case ".tsx":
		return "\ue625" // nf-dev-react (tsx = react component)
	// JavaScript
	case ".js", ".mjs", ".cjs":
		return "\ue60c" // nf-dev-javascript
	case ".jsx":
		return "\ue625" // nf-dev-react
	// Python
	case ".py", ".pyw", ".pyx":
		return "\ue606" // nf-dev-python
	// Rust
	case ".rs":
		return "\ue7a8" // nf-dev-rust
	// Ruby
	case ".rb", ".erb":
		return "\ue21e" // nf-dev-ruby
	// Java / Kotlin
	case ".java":
		return "\ue738" // nf-dev-java
	case ".kt", ".kts":
		return "\ue634" // nf-dev-kotlin
	// C / C++
	case ".c", ".h":
		return "\ue61e" // nf-dev-c
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "\ue61d" // nf-dev-cplusplus
	// C#
	case ".cs":
		return "\uf81a" // nf-mdi-language_csharp
	// Shell
	case ".sh", ".bash":
		return "\uf489" // nf-dev-terminal
	case ".zsh":
		return "\uf489"
	case ".fish":
		return "\uf489"
	case ".ps1", ".psm1":
		return "\uf489"
	// Web
	case ".html", ".htm":
		return "\uf13b" // nf-fa-html5
	case ".css":
		return "\uf13c" // nf-fa-css3
	case ".scss", ".sass":
		return "\ue603" // nf-dev-sass
	case ".less":
		return "\ue758" // nf-dev-less
	// Config / data
	case ".json", ".jsonc":
		return "\ue60b" // nf-seti-json
	case ".yaml", ".yml":
		return "\ue6a8" // nf-seti-yaml
	case ".toml":
		return "\ue6b2" // nf-seti-config
	case ".xml":
		return "\uf72d" // nf-mdi-xml
	case ".csv":
		return "\uf1c3" // nf-fa-file_excel_o
	// Docs
	case ".md", ".mdx":
		return "\ue73e" // nf-dev-markdown
	case ".txt":
		return "\uf15c" // nf-fa-file_text_o
	case ".pdf":
		return "\uf1c1" // nf-fa-file_pdf_o
	// Images
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tiff":
		return "\uf1c5" // nf-fa-file_image_o
	case ".svg":
		return "\ueb8b" // nf-cod-file_media
	case ".ico":
		return "\uf1c5"
	// Go module
	case ".mod", ".sum":
		return "\ue627"
	// Lock files
	case ".lock":
		return "\uf023"
	// Docker
	case ".dockerfile":
		return "\uf308"
	// SQL
	case ".sql":
		return "\uf1c0" // nf-fa-database
	// Wasm
	case ".wasm":
		return "\uf7a3" // nf-fa-cube
	// Vim
	case ".vim":
		return "\ue62b" // nf-dev-vim
	// Lua
	case ".lua":
		return "\ue620" // nf-dev-lua
	// Dart / Flutter
	case ".dart":
		return "\ue798" // nf-dev-dart
	// Swift
	case ".swift":
		return "\ue755" // nf-dev-swift
	// PHP
	case ".php":
		return "\ue608" // nf-dev-php
	// Binary / compiled
	case ".exe", ".bin", ".out", ".elf":
		return "\uf2d0" // nf-mdi-application
	}

	return "\uf15b" // nf-fa-file (default)
}
