package tui

import (
	"fmt"
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
	// ── General ────────────────────────────────────────────────────────────
	{Name: "/help", Description: "Show all available commands and capabilities", Prompt: "List every slash command available and what it does."},
	{Name: "/clear", Description: "Clear chat history", Prompt: ""},
	{Name: "/cancel", Description: "Stop the current agent run", Prompt: ""},
	{Name: "/status", Description: "Show git status of workspace", Prompt: "Run git status and summarise what has changed."},
	{Name: "/diff", Description: "Show current git diff", Prompt: "Show me the current git diff with a short summary of each change."},
	{Name: "/log", Description: "Recent git commit log", Prompt: "Show the last 10 git commits with author, date, and message."},

	// ── Planning ───────────────────────────────────────────────────────────
	{Name: "/plan", Description: "Create a step-by-step plan for the task", Prompt: "Analyse the workspace and create a detailed, numbered implementation plan for the current task. List files you will touch and why."},
	{Name: "/breakdown", Description: "Break a feature into sub-tasks", Prompt: "Break the requested feature into atomic sub-tasks. Each sub-task should be independently implementable and testable."},
	{Name: "/estimate", Description: "Estimate complexity and time", Prompt: "Estimate the complexity (S/M/L/XL) and rough time required for the current task. Explain your reasoning."},

	// ── Code ───────────────────────────────────────────────────────────────
	{Name: "/review", Description: "Code review recent changes", Prompt: "Review the recent code changes. Check for bugs, security issues, performance problems, and style violations. Be specific."},
	{Name: "/refactor", Description: "Suggest refactors for the current file", Prompt: "Analyse the current file and suggest concrete refactoring improvements with before/after examples."},
	{Name: "/fix", Description: "Find and fix bugs in workspace", Prompt: "Scan the workspace for bugs, runtime errors, and logic issues. Fix them one by one and explain each fix."},
	{Name: "/types", Description: "Check and improve type safety", Prompt: "Review the codebase for missing or weak types. Add proper type annotations, interfaces, and generics where beneficial."},
	{Name: "/lint", Description: "Run linter and fix all warnings", Prompt: "Run the project linter, then fix every warning and error it reports."},
	{Name: "/fmt", Description: "Format all source files", Prompt: "Format all source files using the project's formatter (gofmt, prettier, black, etc)."},
	{Name: "/dead", Description: "Find dead code and unused exports", Prompt: "Find all dead code, unused functions, unreachable branches, and unused exports. List them with file:line and suggest removal."},
	{Name: "/deps", Description: "Audit and update dependencies", Prompt: "List all project dependencies, flag outdated or vulnerable ones, and suggest safe upgrades."},

	// ── Testing ────────────────────────────────────────────────────────────
	{Name: "/test", Description: "Run the test suite", Prompt: "Run all tests and report results. If any fail, diagnose and fix them."},
	{Name: "/testgen", Description: "Generate tests for current file", Prompt: "Generate comprehensive unit tests for the current file. Cover happy path, edge cases, and error conditions."},
	{Name: "/coverage", Description: "Show test coverage report", Prompt: "Run the test suite with coverage enabled and report which files and functions lack coverage."},
	{Name: "/e2e", Description: "Write end-to-end tests", Prompt: "Write end-to-end tests for the main user flows of this project."},

	// ── Git / Version Control ──────────────────────────────────────────────
	{Name: "/commit", Description: "Stage and commit all changes with a message", Prompt: "Stage all changes and write a conventional commit message that accurately describes what changed and why."},
	{Name: "/pr", Description: "Draft a pull request description", Prompt: "Draft a pull request title and description for the current branch changes. Include summary, motivation, and testing steps."},
	{Name: "/changelog", Description: "Generate a changelog entry", Prompt: "Generate a CHANGELOG.md entry for the current changes following Keep a Changelog format."},
	{Name: "/branch", Description: "Suggest a branch name for the current task", Prompt: "Suggest a conventional git branch name for the current task (feat/, fix/, chore/, etc)."},
	{Name: "/undo", Description: "Revert last agent change via checkpoint", Prompt: ""},

	// ── Automation ─────────────────────────────────────────────────────────
	{Name: "/automate", Description: "Turn this task into a saved automation", Prompt: "Convert the current task into a reusable automation template. Name it, describe its trigger, and save it to the automation catalog."},
	{Name: "/automations", Description: "List all saved automations", Prompt: "List all automations in the catalog with their name, trigger, and last run time."},
	{Name: "/run", Description: "Run a saved automation by name", Prompt: ""},
	{Name: "/schedule", Description: "Schedule an automation on a cron", Prompt: "Help me schedule an automation to run on a recurring schedule. Ask for the automation name and cron expression."},
	{Name: "/trigger", Description: "Set a trigger for an automation", Prompt: "Configure a trigger (file change, git push, time-based, or manual) for an existing automation."},
	{Name: "/unschedule", Description: "Remove a scheduled automation", Prompt: "List scheduled automations and let me choose one to remove."},

	// ── Docs & Explanation ─────────────────────────────────────────────────
	{Name: "/explain", Description: "Explain the current file or selection", Prompt: "Explain this code in plain English. Describe what it does, how it works, and any non-obvious design decisions."},
	{Name: "/docs", Description: "Generate documentation for the project", Prompt: "Generate clear documentation for the project: README overview, API docs, and inline comments for complex functions."},
	{Name: "/diagram", Description: "Create an architecture diagram", Prompt: "Analyse the project structure and describe an architecture diagram (components, data flow, dependencies) in Mermaid format."},
	{Name: "/readme", Description: "Update or generate README.md", Prompt: "Update README.md to accurately reflect the current state of the project. Include setup, usage, and contribution sections."},

	// ── Security ───────────────────────────────────────────────────────────
	{Name: "/security", Description: "Full security audit of the codebase", Prompt: "Perform a thorough security audit. Check for injection, auth flaws, secrets in code, insecure dependencies, and improper error handling."},
	{Name: "/secrets", Description: "Scan for hardcoded secrets", Prompt: "Scan all source files for hardcoded API keys, passwords, tokens, and secrets. List file:line for each finding."},
	{Name: "/pentest", Description: "Identify attack surface and weak points", Prompt: "Analyse the attack surface of this application. Identify weak points, missing input validation, and potential abuse vectors."},

	// ── Performance ────────────────────────────────────────────────────────
	{Name: "/perf", Description: "Profile and identify bottlenecks", Prompt: "Identify performance bottlenecks in the codebase. Look for O(n²) algorithms, unnecessary allocations, blocking I/O, and caching opportunities."},
	{Name: "/bundle", Description: "Analyse frontend bundle size", Prompt: "Analyse the frontend bundle. Identify large dependencies and suggest code splitting, lazy loading, or lighter alternatives."},

	// ── Database ───────────────────────────────────────────────────────────
	{Name: "/schema", Description: "Show and explain the DB schema", Prompt: "Show the database schema and explain each table, its relationships, and indexes."},
	{Name: "/migrate", Description: "Generate a DB migration", Prompt: "Generate a database migration for the current schema changes. Include up and down migrations."},
	{Name: "/seed", Description: "Generate seed data for development", Prompt: "Generate realistic seed data for all database tables for local development and testing."},

	// ── DevOps ─────────────────────────────────────────────────────────────
	{Name: "/dockerfile", Description: "Write or improve the Dockerfile", Prompt: "Write an optimised, production-ready Dockerfile for this project with multi-stage build, minimal image size, and security best practices."},
	{Name: "/ci", Description: "Generate CI/CD pipeline config", Prompt: "Generate a CI/CD pipeline configuration (GitHub Actions) for this project including build, test, lint, and deploy stages."},
	{Name: "/env", Description: "List all required environment variables", Prompt: "List all environment variables this project requires, their purpose, example values, and whether they are required or optional."},
	{Name: "/deploy", Description: "Deployment checklist and steps", Prompt: "Generate a deployment checklist for this project. Include pre-deploy checks, migration steps, and rollback procedure."},

	// ── SSH Remote ─────────────────────────────────────────────────────────
	{Name: "/ssh", Description: "Manage SSH remote hosts (Ctrl+H)", Prompt: ""},
	{Name: "/ssh-connect", Description: "Connect to a remote host via SSH tunnel", Prompt: ""},
	{Name: "/ssh-disconnect", Description: "Disconnect from remote, return to local daemon", Prompt: ""},
	{Name: "/setup", Description: "Configure a real model provider", Prompt: ""},
	{Name: "/model", Description: "Show the active model", Prompt: ""},
	{Name: "/cwd", Description: "Show the workspace this session is bound to", Prompt: ""},
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
	ti.Prompt = "› "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(colorViolet).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(colorWhite)
	ti.Placeholder = "Ask Rove Code to build, fix, or explain…"
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorDim)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(colorYellow)
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
	b.input.Width = maxInt(w-4, 8)
}

// DesiredHeight lets the root layout reserve room for the command palette.
func (b *InputBar) DesiredHeight() int {
	if b.slashOpen {
		return 11
	}
	return 2
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
	w := maxInt(b.width, 12)
	rows := make([]string, 0, b.DesiredHeight())
	if b.slashOpen {
		rows = append(rows, b.renderSlashPopup(w)...)
	}
	rows = append(rows, styleSep.Render(strings.Repeat("─", w)))
	rows = append(rows, b.renderInputLine(w))
	return lipgloss.NewStyle().Width(w).Background(colorBgPanel).Render(strings.Join(rows, "\n"))
}

func (b *InputBar) renderInputLine(w int) string {
	view := b.input.View()
	if lipgloss.Width(view) > w-3 {
		return truncateVisible(view, w-3)
	}
	return "  " + view
}

func (b *InputBar) renderSlashPopup(maxW int) []string {
	filtered := b.filteredCommands()
	rows := []string{ruledHeader("commands", maxW, fmt.Sprintf("%d", len(filtered)), true)}
	if len(filtered) == 0 {
		rows = append(rows, "  "+styleMeta.Render("no commands matched"))
		for len(rows) < 9 {
			rows = append(rows, "")
		}
		return rows
	}

	start := 0
	if len(filtered) > 8 {
		start = b.slashCursor - 3
		if start < 0 {
			start = 0
		}
		if start+8 > len(filtered) {
			start = len(filtered) - 8
		}
	}
	end := start + 8
	if end > len(filtered) {
		end = len(filtered)
	}
	nameW := 18
	for index := start; index < end; index++ {
		cmd := filtered[index]
		name := cmd.Name
		if len(name) < nameW {
			name += strings.Repeat(" ", nameW-len(name))
		}
		description := cmd.Description
		line := name + " " + description
		line = truncate(line, maxW-5)
		if index == b.slashCursor {
			marker := lipgloss.NewStyle().Foreground(colorViolet).Bold(true).Render("›")
			rows = append(rows, " "+marker+" "+styleSelected.Width(maxW-4).Render(line))
		} else {
			rows = append(rows, "   "+styleMuted.Render(line))
		}
	}
	for len(rows) < 9 {
		rows = append(rows, "")
	}
	return rows
}

func (b *InputBar) Model() *textinput.Model {
	return &b.input
}
