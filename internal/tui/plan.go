package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// TodoItem is a plan step.
type TodoItem struct {
	Text     string
	Done     bool
	Current  bool
	Priority string // "high", "low", ""
}

// PlanPanel renders the plan (todo list) and usage bar in the right rail.
type PlanPanel struct {
	todos    []TodoItem
	stepN    int
	stepM    int
	active   bool
	width    int
	height   int

	// Timer display (set externally)
	timerStr string

	// Approval pending flag
	needsApproval bool

	// Usage stats
	promptTokens     int64
	completionTokens int64
	totalTokens      int64
	contextPct       float64
	costUSD          float64
	contextLimit     int64
	modelName        string
}

func NewPlanPanel() *PlanPanel {
	return &PlanPanel{contextLimit: 128000, modelName: "claude-sonnet-4"}
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

func (p *PlanPanel) SetTimer(s string) {
	p.timerStr = s
}

func (p *PlanPanel) SetNeedsApproval(v bool) {
	p.needsApproval = v
}

func (p *PlanPanel) SetModelName(s string) {
	if s != "" {
		p.modelName = s
	}
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
	w := p.width
	if w < 4 {
		w = 4
	}
	h := p.height
	if h < 6 {
		h = 6
	}

	// Split: plan top ~65%, usage bottom ~35%
	usageH := h * 35 / 100
	if usageH < 5 {
		usageH = 5
	}
	planH := h - usageH
	if planH < 3 {
		planH = 3
	}

	planSection := p.renderPlan(w, planH)
	usageSection := p.renderUsage(w, usageH)

	content := planSection + usageSection

	return lipgloss.NewStyle().
		Width(w).
		Height(h).
		Background(colorBg).
		Render(content)
}

func (p *PlanPanel) renderPlan(w, h int) string {
	innerW := w - 1
	if innerW < 2 {
		innerW = 2
	}

	var rows []string

	// Header line: thin separator
	rows = append(rows, styleSep.Render(strings.Repeat("─", w)))

	// Title line: "plan" + optional "needs you" + step counter + timer
	titleParts := styleDim.Render("plan")

	if p.needsApproval {
		titleParts += "  " + styleHighlight.Render("needs you")
	}

	if p.stepM > 0 {
		titleParts += "  " + styleDim.Render(fmt.Sprintf("%d/%d", p.stepN, p.stepM))
	}

	if p.timerStr != "" {
		titleParts += "  " + styleDim.Render(p.timerStr)
	}

	rows = append(rows, " "+titleParts)

	// Items
	if len(p.todos) == 0 {
		rows = append(rows, " "+styleDim.Render("(waiting)"))
	}

	for _, item := range p.todos {
		row := p.renderPlanItem(item, innerW)
		rows = append(rows, " "+row)
		if len(rows) >= h {
			break
		}
	}

	// Pad to height
	for len(rows) < h {
		rows = append(rows, "")
	}
	if len(rows) > h {
		rows = rows[:h]
	}

	return strings.Join(rows, "\n") + "\n"
}

func (p *PlanPanel) renderPlanItem(item TodoItem, maxW int) string {
	// Diamond icons: ◆ filled = done or active, ◇ empty = pending
	var icon string
	var textStyle lipgloss.Style

	switch {
	case item.Done:
		icon = lipgloss.NewStyle().Foreground(colorGreen).Render("◆")
		textStyle = styleDim
	case item.Current:
		icon = lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render("◆")
		textStyle = lipgloss.NewStyle().Foreground(colorWhite)
	default:
		icon = styleDim.Render("◇")
		textStyle = styleDim
	}

	// Priority tag
	var priorityTag string
	switch item.Priority {
	case "high":
		priorityTag = " " + lipgloss.NewStyle().Foreground(colorYellow).Render("high")
	case "low":
		// no tag, just dim text already
	}

	// Available text width: icon(1) + space(1) + priorityTag
	tagW := 0
	if item.Priority == "high" {
		tagW = 5 // " high"
	}
	textW := maxW - 2 - tagW
	if textW < 4 {
		textW = 4
	}

	text := item.Text
	runes := []rune(text)
	if len(runes) > textW {
		text = string(runes[:textW-1]) + "…"
	}

	return icon + " " + textStyle.Render(text) + priorityTag
}

func (p *PlanPanel) renderUsage(w, h int) string {
	var rows []string

	// Separator
	rows = append(rows, styleSep.Render(strings.Repeat("─", w)))

	// Title
	rows = append(rows, " "+styleDim.Render("usage"))

	// Context bar
	barW := w - 3
	if barW < 2 {
		barW = 2
	}
	filled := int(float64(barW) * p.contextPct)
	if filled > barW {
		filled = barW
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barW-filled)
	barColor := colorBlue
	if p.contextPct > 0.8 {
		barColor = colorYellow
	}
	if p.contextPct > 0.95 {
		barColor = colorRed
	}
	pctStr := fmt.Sprintf("%.1f%%", p.contextPct*100)
	barStr := lipgloss.NewStyle().Foreground(barColor).Render(bar)
	rows = append(rows, " "+barStr+" "+styleDim.Render(pctStr))

	// Tokens line: "Tokens: 6.7k  5.1k/1.6k"
	var tokenLine string
	if p.totalTokens > 0 {
		tok := formatTokens(p.totalTokens)
		prompt := formatTokens(p.promptTokens)
		comp := formatTokens(p.completionTokens)
		tokenLine = fmt.Sprintf("Tokens: %s  %s/%s", tok, prompt, comp)
	} else {
		tokenLine = "Tokens: —"
	}
	rows = append(rows, " "+styleDim.Render(tokenLine))

	// Cost + model line
	var costLine string
	if p.costUSD > 0 {
		costLine = fmt.Sprintf("Cost: $%.3f  %s", p.costUSD, p.modelName)
	} else {
		costLine = "Cost: —  " + p.modelName
	}
	rows = append(rows, " "+styleDim.Render(costLine))

	// Pad
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