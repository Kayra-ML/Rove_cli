package tui

import (
	"strings"
	"testing"

	"github.com/aether-dev/aether/internal/types"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestInputHasSinglePrompt(t *testing.T) {
	input := NewInputBar()
	input.SetSize(120, 2)
	view := input.Render()
	if strings.Contains(view, "> >") || strings.Contains(view, "› ›") {
		t.Fatalf("input renders duplicate prompt: %q", view)
	}
	if !strings.Contains(view, "Ask Rove Code") {
		t.Fatalf("input is missing Rove Code placeholder: %q", view)
	}
}

func TestRootRenderFitsTerminal(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 30}, {160, 44}, {88, 24}, {72, 20}} {
		m := NewModel(nil)
		m.width, m.height = size.w, size.h
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) > size.h {
			t.Fatalf("%dx%d render has %d lines", size.w, size.h, len(lines))
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got > size.w {
				t.Fatalf("%dx%d row %d width=%d", size.w, size.h, i, got)
			}
		}
	}
}

func TestRootRenderHasCoreSurfaces(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 120, 30
	view := stripSimpleANSI(m.View())
	for _, want := range []string{"ROVE CODE", "messages", "plan", "usage", "Ask Rove Code"} {
		if !strings.Contains(view, want) {
			t.Fatalf("render missing %q", want)
		}
	}
}

func TestFirstPromptCreatesSessionAndIsNotDropped(t *testing.T) {
	m := NewModel(&IPCClient{})
	m.inputBar.input.SetValue("fix the failing test")

	cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("first prompt did not request session creation")
	}
	if m.pendingPrompt != "fix the failing test" {
		t.Fatalf("pending prompt = %q", m.pendingPrompt)
	}

	_, _ = m.Update(SessionListMsg{
		Sessions:         []types.Session{{ID: types.ID("session-1")}},
		CreatedSessionID: "session-1",
	})
	if m.currentSessID != "session-1" {
		t.Fatalf("current session = %q", m.currentSessID)
	}
	if m.pendingPrompt != "" {
		t.Fatalf("pending prompt was not consumed: %q", m.pendingPrompt)
	}
	if len(m.messagesPanel.messages) != 1 || m.messagesPanel.messages[0].Content != "fix the failing test" {
		t.Fatalf("first prompt missing from chat: %#v", m.messagesPanel.messages)
	}
}

func TestStreamDeltasCoalesceAndToolRowsCompleteInPlace(t *testing.T) {
	panel := NewMessagesPanel()
	panel.AppendAssistantDelta("hello ")
	panel.AppendAssistantDelta("world")
	if len(panel.messages) != 1 || panel.messages[0].Content != "hello world" {
		t.Fatalf("stream chunks were not coalesced: %#v", panel.messages)
	}
	panel.AddMessage(DisplayMessage{Role: types.RoleTool, ToolName: "read_file"})
	panel.CompleteTool("read_file", "main.go · 42 lines", false)
	if len(panel.messages) != 2 || panel.messages[1].ToolExtra != "main.go · 42 lines" {
		t.Fatalf("tool result created noise instead of completing row: %#v", panel.messages)
	}
}

func TestPlanTracksRealRunPhases(t *testing.T) {
	plan := NewPlanPanel()
	plan.StartRun("Analyze request")
	plan.AdvanceRun("read main.go")
	plan.CompleteRun()
	if len(plan.todos) != 2 || !plan.todos[0].Done || !plan.todos[1].Done || plan.todos[1].Current {
		t.Fatalf("unexpected plan state: %#v", plan.todos)
	}
}

func TestDisplayModelHidesFakeProvider(t *testing.T) {
	if got := displayModel(types.Agent{Provider: "fake", Model: "fake"}); got != "no model configured" {
		t.Fatalf("fake/fake = %q", got)
	}
	if got := displayModel(types.Agent{Provider: "openai", Model: "gpt-5"}); got != "openai/gpt-5" {
		t.Fatalf("named model = %q", got)
	}
}

func TestUsageMessageFeedsRightRail(t *testing.T) {
	m := NewModel(&IPCClient{})
	_, _ = m.Update(UsageMsg{PromptTokens: 1200, CompletionTokens: 300, TotalTokens: 1500, Calls: 2})
	if m.planPanel.totalTokens != 1500 || m.planPanel.calls != 2 {
		t.Fatalf("usage not applied: %#v", m.planPanel)
	}
}

func TestCommandPaletteReservesHeight(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 120, 30
	m.inputBar.OpenSlash()
	view := m.View()
	if got := len(strings.Split(view, "\n")); got > m.height {
		t.Fatalf("palette render overflow: got %d rows, max %d", got, m.height)
	}
	if !strings.Contains(stripSimpleANSI(view), "commands") {
		t.Fatal("palette header missing")
	}
}
