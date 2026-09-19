package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type TodoItem struct {
	Text     string
	Done     bool
	Current  bool
	Priority string
}

type PlanPanel struct {
	todos         []TodoItem
	stepN         int
	stepM         int
	active        bool
	width         int
	height        int
	timerStr      string
	needsApproval bool

	promptTokens     int64
	completionTokens int64
	totalTokens      int64
	contextPct       float64
	calls            int64
	contextLimit     int64
	modelName        string
}

func NewPlanPanel() *PlanPanel {
	return &PlanPanel{contextLimit: 128000, modelName: "model not set"}
}

func (p *PlanPanel) SetSize(w, h int)          { p.width, p.height = w, h }
func (p *PlanPanel) SetActive(active bool)     { p.active = active }
func (p *PlanPanel) SetTodos(todos []TodoItem) { p.todos = todos }
func (p *PlanPanel) SetStep(n, m int)          { p.stepN, p.stepM = n, m }
func (p *PlanPanel) SetTimer(s string)         { p.timerStr = s }
func (p *PlanPanel) SetNeedsApproval(v bool)   { p.needsApproval = v }

func (p *PlanPanel) StartRun(label string) {
	p.todos = []TodoItem{{Text: label, Current: true}}
	p.stepN, p.stepM = 1, 0
}

func (p *PlanPanel) AdvanceRun(label string) {
	label = strings.TrimSpace(label)
	if label == "" {
		return
	}
	if len(p.todos) > 0 && p.todos[len(p.todos)-1].Text == label {
		return
	}
	for i := range p.todos {
		if p.todos[i].Current {
			p.todos[i].Current = false
			p.todos[i].Done = true
		}
	}
	p.todos = append(p.todos, TodoItem{Text: label, Current: true})
	p.stepN, p.stepM = len(p.todos), 0
}

func (p *PlanPanel) CompleteRun() {
	for i := range p.todos {
		p.todos[i].Current = false
		p.todos[i].Done = true
	}
	p.stepN, p.stepM = len(p.todos), len(p.todos)
}

func (p *PlanPanel) SetModelName(s string) {
	if strings.TrimSpace(s) != "" {
		p.modelName = s
	}
}

func (p *PlanPanel) SetUsage(prompt, completion, total, calls int64) {
	p.promptTokens, p.completionTokens, p.totalTokens, p.calls = prompt, completion, total, calls
	if p.contextLimit > 0 {
		p.contextPct = float64(total) / float64(p.contextLimit)
		if p.contextPct > 1 {
			p.contextPct = 1
		}
	}
}

func (p *PlanPanel) Render() string {
	w := maxInt(p.width, 18)
	h := maxInt(p.height, 10)
	// Usage mini: header + bar + 3 data lines = 5 rows max
	usageH := 5
	planH := h - usageH
	rows := append(p.renderPlan(w, planH), p.renderUsage(w, usageH)...)
	for len(rows) < h {
		rows = append(rows, "")
	}
	if len(rows) > h {
		rows = rows[:h]
	}
	clamped := make([]string, len(rows))
	for i, r := range rows {
		clamped[i] = fitVisible(r, w)
	}
	return strings.Join(clamped, "\n")
}

func (p *PlanPanel) renderPlan(w, h int) []string {
	meta := ""
	if p.stepM > 0 {
		meta = fmt.Sprintf("%d/%d", p.stepN, p.stepM)
	} else if p.stepN > 0 {
		meta = fmt.Sprintf("step %d", p.stepN)
	}
	rows := []string{ruledHeader("plan", w, meta, p.active)}

	if p.needsApproval {
		status := styleHighlight.Bold(true).Render("◆ needs you")
		if p.timerStr != "" {
			status += "  " + styleMeta.Render(p.timerStr)
		}
		rows = append(rows, "  "+truncateVisible(status, w-3), "")
	} else if p.timerStr != "" {
		rows = append(rows, "  "+styleSuccess.Render("● running")+"  "+styleMeta.Render(p.timerStr), "")
	} else {
		rows = append(rows, "")
	}

	if len(p.todos) == 0 {
		rows = append(rows,
			"  "+styleDim.Render("◇")+" "+styleMuted.Render("no active plan"),
			"    "+styleMeta.Render("updates while Rove works"),
		)
	} else {
		for _, item := range p.todos {
			if len(rows) >= h {
				break
			}
			rows = append(rows, "  "+p.renderPlanItem(item, w-3))
		}
	}
	for len(rows) < h {
		rows = append(rows, "")
	}
	if len(rows) > h {
		rows = rows[:h]
	}
	return rows
}

func (p *PlanPanel) renderPlanItem(item TodoItem, maxW int) string {
	icon := iconPending
	textStyle := styleMuted
	if item.Done {
		icon = iconDone
		textStyle = styleMeta
	} else if item.Current {
		icon = iconCurrent
		textStyle = lipgloss.NewStyle().Foreground(colorWhite)
	}
	tag := ""
	if item.Priority == "high" {
		tag = " " + styleHighlight.Render("high")
	} else if item.Priority == "low" {
		tag = " " + styleMeta.Render("low")
	}
	available := maxW - 2 - lipgloss.Width(tag)
	if available < 3 {
		available = 3
	}
	return icon + " " + textStyle.Render(truncate(item.Text, available)) + tag
}

func (p *PlanPanel) renderUsage(w, h int) []string {
	rows := []string{sectionHeader("usage", w, fmt.Sprintf("%.0f%%", p.contextPct*100), false)}
	barW := w - 4
	if barW < 8 {
		barW = 8
	}
	filled := int(float64(barW) * p.contextPct)
	filled = clampInt(filled, 0, barW)
	barColor := colorGreen
	if p.contextPct >= .70 {
		barColor = colorYellow
	}
	if p.contextPct >= .90 {
		barColor = colorRed
	}
	bar := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("=", filled))
	bar += styleFrame.Render(strings.Repeat("-", barW-filled))
	rows = append(rows, "  "+bar)

	total := formatTokens(p.totalTokens)
	rows = append(rows,
		"  "+styleMuted.Render("tok")+" "+lipgloss.NewStyle().Foreground(colorWhite).Render(total)+"  "+styleMeta.Render("calls "+fmt.Sprintf("%d", p.calls)),
		"  "+styleMeta.Render(truncate(p.modelName, w-4)),
	)
	for len(rows) < h {
		rows = append(rows, "")
	}
	if len(rows) > h {
		rows = rows[:h]
	}
	return rows
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
