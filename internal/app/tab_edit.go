package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type pasteDoneMsg struct {
	tab  int
	text string
	err  error
}

func (m pasteDoneMsg) tabID() int { return m.tab }

// editorShortcut handles the line editing keys of the SQL editor; handled is false for any other key.
func (t *Tab) editorShortcut(key tea.KeyMsg) (cmd tea.Cmd, handled bool) {
	switch key.String() {
	case "ctrl+d":
		t.duplicateLine()
	case "ctrl+x":
		cmd = t.cutLine()
	case "ctrl+v":
		cmd = t.paste()
	default:
		return nil, false
	}
	t.suggest = nil
	return cmd, true
}

// cursor returns the editor's lines and the cursor's line and column (in runes).
func (t *Tab) cursor() (lines []string, row, col int) {
	lines = strings.Split(t.editor.Value(), "\n")
	li := t.editor.LineInfo()
	return lines, min(t.editor.Line(), len(lines)-1), li.StartColumn + li.ColumnOffset
}

// setLines replaces the editor text and puts the cursor at row and col.
func (t *Tab) setLines(lines []string, row, col int) {
	t.editor.SetValue(strings.Join(lines, "\n"))
	// SetValue leaves the cursor at the end; CursorUp moves by visual row, so loop until the logical line matches.
	for t.editor.Line() > row {
		t.editor.CursorUp()
	}
	t.editor.SetCursor(col)
}

func (t *Tab) duplicateLine() {
	lines, row, col := t.cursor()
	lines = append(lines[:row+1], append([]string{lines[row]}, lines[row+1:]...)...)
	t.setLines(lines, row+1, col)
}

// cutLine removes the current line and puts it, with its newline, on the clipboard.
func (t *Tab) cutLine() tea.Cmd {
	lines, row, _ := t.cursor()
	cut := lines[row]
	if len(lines) == 1 {
		if cut == "" {
			return nil
		}
		t.setLines([]string{""}, 0, 0)
	} else {
		lines = append(lines[:row], lines[row+1:]...)
		t.setLines(lines, min(row, len(lines)-1), 0)
	}
	return t.copyAs("Cut", fmt.Sprintf("line %d", row+1), cut+"\n")
}

func (t *Tab) paste() tea.Cmd {
	clipboard, id := t.session.clipboard, t.id
	return func() tea.Msg {
		text, err := clipboard.Paste()
		return pasteDoneMsg{tab: id, text: text, err: err}
	}
}

// handlePaste inserts the clipboard at the cursor, falling back to queryme's own last copy without a clipboard tool.
func (t *Tab) handlePaste(msg pasteDoneMsg) {
	text := msg.text
	switch {
	case msg.err == nil:
		t.notice = ""
	case t.session.lastCopied != "":
		text = t.session.lastCopied
		t.notice = "Pasted queryme's last copy (system clipboard: " + msg.err.Error() + ")"
	default:
		t.notice = "Nothing to paste: " + msg.err.Error()
		return
	}
	t.editor.InsertString(text)
}
