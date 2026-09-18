package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// TodoItem is a plan step.
type TodoItem struct {
	Text    string
	Done    bool
	Current bool
}

// PlanPanel renders the plan (todo list) and usage bar.
type PlanPanel struct {
	todos    []TodoItem
	stepN    int
	stepM    int
	active   bool
	width    int
	height   int

	// Usage stats
	promptTokens     int64
	completionTokens int64
	totalTokens      int64
	contextPct       float64
	costUSD          float64
	contextLimit     int64
}

func NewPlanPanel() *PlanPanel {
	return &PlanPanel{contextLimit: 128000}
}

func (p *PlanPanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *PlanPanel) SetActive(active bool) {
	p.active = active
}

func (p *PlanPanel) SetTodos(todos []TodoItem) {
	p.todos = todos
}

func (p *PlanPanel) SetStep(n, m int) {
	p.stepN = n
	p.stepM = m
}

func (p *PlanPanel) SetUsage(prompt, completion, total int64, cost float64) {
	p.promptTokens = prompt
	p.completionTokens = completion
	p.totalTokens = total
	p.costUSD = cost
	if p.contextLimit > 0 {
		p.contextPct = float64(total) / float64(p.contextLimit)
		if p.contextPct > 1.0 {
			p.contextPct = 1.0
		}
	}
}

func (p *PlanPanel) Render() string {
	innerW := p.width - 4
	innerH := p.height - 2
	if innerW < 4 {
		innerW = 4
	}
	if innerH < 2 {
		innerH = 2
	}

	// Split height: plan gets top portion, usage gets bottom ~7 rows
	usageH := 7
	planH := innerH - usageH
	if planH < 2 {
		planH = 2
		usageH = innerH - planH
	}

	planSection := p.renderPlan(innerW, planH)
	usageSection := p.renderUsage(innerW, usageH)

	content := planSection + "\n" + usageSection

	return panelStyle(p.active).
		Width(innerW).
		Height(innerH).
		Render(content)
}

func (p *PlanPanel) renderPlan(w, h int) string {
	var rows []string

	stepStr := ""
	if p.stepM > 0 {
		stepStr = fmt.Sprintf(" %d/%d", p.stepN, p.stepM)
	}
	rows = append(rows, titleStyle(p.active).Render("Plan"+stepStr))

	if len(p.todos) == 0 {
		rows = append(rows, styleDim.Render("  (no plan)"))
	}

	for _, item := range p.todos {
		check := "☐"
		style := styleDim
		if item.Done {
			check = "☑"
			style = styleSuccess
		}
		if item.Current {
			check = "▶"
			style = lipgloss.NewStyle().Foreground(colorHighlight).Bold(true)
		}
		text := item.Text
		if len(text) > w-4 {
			text = text[:w-5] + "…"
		}
		rows = append(rows, style.Render(fmt.Sprintf(" %s %s", check, text)))
		if len(rows) >= h {
			break
		}
	}

	for len(rows) < h {
		rows = append(rows, "")
	}

	return strings.Join(rows[:h], "\n")
}

func (p *PlanPanel) renderUsage(w, h int) string {
	var rows []string

	rows = append(rows, styleDim.Render(strings.Repeat("─", w)))
	rows = append(rows, titleStyle(false).Render("Usage"))

	// Context bar
	barW := w - 2
	if barW < 4 {
		barW = 4
	}
	filled := int(float64(barW) * p.contextPct)
	if filled > barW {
		filled = barW
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barW-filled)
	barStyle := lipgloss.NewStyle().Foreground(colorContextBar)
	pctStr := fmt.Sprintf("%.1f%%", p.contextPct*100)
	rows = append(rows, " "+barStyle.Render(bar))
	rows = append(rows, styleDim.Render(fmt.Sprintf(" ctx: %s", pctStr)))

	// Tokens
	if p.totalTokens > 0 {
		rows = append(rows, styleDim.Render(fmt.Sprintf(" tok: %s", formatTokens(p.totalTokens))))
	} else {
		rows = append(rows, styleDim.Render(" tok: —"))
	}

	// Cost
	if p.costUSD > 0 {
		rows = append(rows, styleDim.Render(fmt.Sprintf(" cost: $%.4f", p.costUSD)))
	} else {
		rows = append(rows, styleDim.Render(" cost: —"))
	}

	for len(rows) < h {
		rows = append(rows, "")
	}
	if len(rows) > h {
		rows = rows[:h]
	}

	return strings.Join(rows, "\n")
}

func formatTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}