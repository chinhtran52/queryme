package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/chinhtran52/queryme/internal/ai"
	"github.com/chinhtran52/queryme/internal/ui"
)

const (
	refreshCommand = "/refresh"
	promptBoxLines = 3                  // lines of text the request box shows; longer text wraps and scrolls
	promptLines    = promptBoxLines + 1 // plus the label line above it
	aiTickEvery    = 350 * time.Millisecond
)

type aiDoneMsg struct {
	tab     int
	request string
	reply   ai.Reply
	err     error
}

// aiTickMsg advances the "..." animation while a request is in flight.
type aiTickMsg struct{ tab int }

func aiTick(tab int) tea.Cmd {
	return tea.Tick(aiTickEvery, func(time.Time) tea.Msg { return aiTickMsg{tab: tab} })
}

// openTabMsg asks the app for a new tab holding AI-written SQL, so a non-empty editor is never overwritten.
type openTabMsg struct {
	sql, title, aiSession, notes string
}

// newPrompt is a wrapping multi-line box; Enter sends instead of adding a line.
func newPrompt() textarea.Model {
	prompt := textarea.New()
	prompt.Prompt = ""
	prompt.ShowLineNumbers = false
	prompt.Placeholder = "Describe the query to write, or ask to fix or explain the SQL above"
	prompt.CharLimit = 2000
	prompt.KeyMap.InsertNewline.SetEnabled(false)
	focused := lipgloss.NewStyle().Background(ui.FocusedBackground)
	prompt.FocusedStyle.Base = focused
	prompt.FocusedStyle.CursorLine = focused
	prompt.FocusedStyle.Placeholder = focused.Faint(true)
	prompt.BlurredStyle.Placeholder = lipgloss.NewStyle().Faint(true)
	prompt.Blur()
	return prompt
}

// promptView draws the label line and the request box; both get the focus background while focused.
func (t *Tab) promptView() string {
	base := lipgloss.NewStyle()
	if t.focus == focusPrompt {
		base = base.Background(ui.FocusedBackground)
	}
	label, hint, color := "✦ Ask AI", "  ctrl+k · enter to send · esc back", lipgloss.Color("12")
	if t.asking {
		label, hint, color = "✦ Asking Claude"+strings.Repeat(".", t.aiFrame%3+1), "  ctrl+c to cancel", lipgloss.Color("10")
	}
	line := base.Bold(true).Foreground(color).Render(label) + base.Faint(true).Render(ui.KeyLabels(hint))
	line += base.Render(strings.Repeat(" ", max(t.width-lipgloss.Width(line), 0)))
	box := t.prompt.View()
	if t.focus == focusPrompt {
		// The textarea's placeholder resets styling mid-line, which would end the background early.
		box = ui.KeepBackground(box, base)
	}
	return line + "\n" + box
}

func (t *Tab) focusPrompt() tea.Cmd {
	t.focus, t.suggest = focusPrompt, nil
	t.editor.Blur()
	t.results.Blur()
	return t.prompt.Focus()
}

func (t *Tab) updatePrompt(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "enter":
		return t.ask()
	case "esc":
		return t.focusEditor()
	}
	var cmd tea.Cmd
	t.prompt, cmd = t.prompt.Update(key)
	return cmd
}

// ask sends the prompt to Claude with the editor SQL and the last error; the answer never runs by itself.
func (t *Tab) ask() tea.Cmd {
	request := strings.TrimSpace(t.prompt.Value())
	switch {
	case request == "" || t.asking:
		return nil
	case request == refreshCommand:
		t.session.schema.Reset()
		t.prompt.Reset()
		t.notice = "Reloading the schema for AI and autocomplete"
		return loadSchema(t.session.schema)
	}
	lastError := ""
	if t.err != nil && !errors.Is(t.err, errCancelled) {
		lastError = t.err.Error()
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.asking, t.aiCancel, t.notice = true, cancel, ""
	t.aiFrame = 0
	assistant, id := t.session.ai, t.id
	req := ai.Request{Prompt: request, Editor: t.editor.Value(), LastError: lastError, SessionID: t.aiSession}
	return tea.Batch(func() tea.Msg {
		reply, err := assistant.Ask(ctx, req)
		return aiDoneMsg{tab: id, request: request, reply: reply, err: err}
	}, aiTick(id))
}

func (t *Tab) handleAIDone(msg aiDoneMsg) tea.Cmd {
	t.asking, t.aiCancel = false, nil
	switch {
	case errors.Is(msg.err, context.Canceled):
		t.notice = "AI request cancelled"
		return nil
	case msg.err != nil:
		t.showNotes("AI request failed", msg.err.Error())
		return nil
	}
	t.prompt.Reset()
	reply := msg.reply
	if reply.SQL == "" {
		t.aiSession = reply.SessionID
		t.showNotes("✦ AI", reply.Notes)
		return t.focusResults()
	}
	if strings.TrimSpace(t.editor.Value()) != "" {
		return func() tea.Msg {
			return openTabMsg{sql: reply.SQL, title: "AI: " + msg.request, aiSession: reply.SessionID, notes: reply.Notes}
		}
	}
	t.aiSession = reply.SessionID
	t.editor.SetValue(reply.SQL)
	t.notice = "AI wrote the query: review it, then ctrl+r to run"
	if reply.Notes != "" {
		t.showNotes("✦ AI", reply.Notes)
	}
	return t.focusEditor()
}

func (t *Tab) showNotes(title, text string) {
	t.menu = nil
	t.panel = ui.NewNotesPanel(title, text, t.width, t.resultsHeight)
}
