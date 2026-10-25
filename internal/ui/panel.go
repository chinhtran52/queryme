package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/chinhtran52/queryme/internal/db"
)

// TextPanel fills the results pane with scrollable text under a title; Esc closes it.
type TextPanel struct {
	title    string
	render   func(width int) string
	viewport viewport.Model
}

func NewTextPanel(title string, render func(width int) string, width, height int) *TextPanel {
	p := &TextPanel{title: title, render: render, viewport: viewport.New(0, 0)}
	p.SetSize(width, height)
	return p
}

// NewNotesPanel shows plain text, such as an AI answer, wrapped to the pane.
func NewNotesPanel(title, text string, width, height int) *TextPanel {
	return NewTextPanel(title, func(w int) string {
		return lipgloss.NewStyle().Width(max(w-2, 1)).PaddingLeft(1).Render(text)
	}, width, height)
}

// NewRowViewer shows every column of one row untruncated, with JSON values pretty-printed.
func NewRowViewer(res *db.Result, row, width, height int) *TextPanel {
	title := fmt.Sprintf("Row %d of %d", row+1, len(res.Rows))
	return NewTextPanel(title, func(w int) string { return rowDetails(res, row, w) }, width, height)
}

// SetSize re-wraps the text for the new width.
func (p *TextPanel) SetSize(width, height int) {
	p.viewport.Width, p.viewport.Height = width, max(height-1, 1)
	p.viewport.SetContent(p.render(width))
}

// Update returns false once the panel is closed.
func (p *TextPanel) Update(msg tea.Msg) bool {
	if key, ok := msg.(tea.KeyMsg); ok && (key.String() == "esc" || key.String() == "q") {
		return false
	}
	p.viewport, _ = p.viewport.Update(msg)
	return true
}

func (p *TextPanel) View() string {
	header := HeaderStyle.Render(p.title) + DimStyle.Render("  ↑↓ scroll · esc close")
	return header + "\n" + p.viewport.View()
}

func rowDetails(res *db.Result, row, width int) string {
	wrap := lipgloss.NewStyle().Width(max(width-2, 1)).PaddingLeft(2)
	var b strings.Builder
	for i, name := range res.Columns {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render(name) + "\n")
		value := wrap.Render(prettyJSON(res.Cell(row, i)))
		if res.Rows[row][i] == nil {
			value = wrap.Faint(true).Italic(true).Render(nullText)
		}
		b.WriteString(value + "\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// prettyJSON indents JSON objects and arrays and leaves anything else as is.
func prettyJSON(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return value
	}
	var out bytes.Buffer
	if json.Indent(&out, []byte(trimmed), "", "  ") != nil {
		return value
	}
	return out.String()
}
