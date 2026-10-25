package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/chinhtran52/queryme/internal/ui"
)

// cursorOffset is the editor cursor as a rune offset into the whole SQL text.
func (t *Tab) cursorOffset() int {
	lines := strings.Split(t.editor.Value(), "\n")
	row := min(t.editor.Line(), len(lines)-1)
	offset := 0
	for _, line := range lines[:row] {
		offset += len([]rune(line)) + 1
	}
	li := t.editor.LineInfo()
	return offset + li.StartColumn + li.ColumnOffset
}

// refreshSuggestions follows a text change in the editor; it hides the list when nothing completes.
func (t *Tab) refreshSuggestions() {
	word, items := t.session.completer.Suggest(t.editor.Value(), t.cursorOffset())
	if len(items) == 0 {
		t.suggest = nil
		return
	}
	t.suggest, t.suggestWord = ui.NewSuggestList(items), word
}

// handleSuggestKey takes the keys that drive an open suggestion list; handled is false for everything else.
func (t *Tab) handleSuggestKey(key tea.KeyMsg) (handled bool) {
	switch key.String() {
	case "tab":
		t.acceptSuggestion()
	case "down", "ctrl+n":
		t.suggest.Move(1)
	case "up", "ctrl+p":
		t.suggest.Move(-1)
	case "esc":
		t.suggest = nil
	default:
		return false
	}
	return true
}

// acceptSuggestion replaces the partial word before the cursor with the selected name.
func (t *Tab) acceptSuggestion() {
	chosen := t.suggest.Selected()
	for range []rune(t.suggestWord) {
		t.editor, _ = t.editor.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	t.editor.InsertString(chosen.Text)
	t.suggest = nil
}

// suggestOrigin places the list under the start of the partial word, or above it when there is no room below.
// shortcut: the textarea does not expose its scroll offset, so the cursor row is estimated from the wrapped lines
// before it and pinned to the last visible row once the text is taller than the editor.
func (t *Tab) suggestOrigin(list string, screenHeight int) (int, int) {
	lines := strings.Split(t.editor.Value(), "\n")
	textWidth := max(t.editor.Width(), 1)
	row := 0
	for _, line := range lines[:min(t.editor.Line(), len(lines)-1)] {
		row += max(1, (runewidth.StringWidth(line)+textWidth-1)/textWidth)
	}
	li := t.editor.LineInfo()
	row = min(row+li.RowOffset, t.editorHeight-1)

	w, h := lipgloss.Width(list), lipgloss.Height(list)
	x := t.editorGutter() + li.ColumnOffset - runewidth.StringWidth(t.suggestWord)
	x = clampInt(x, 0, max(t.width-w, 0))
	if y := row + 1; y+h <= screenHeight {
		return x, y
	}
	return x, max(row-h, 0)
}

func clampInt(v, low, high int) int { return min(max(v, low), high) }
