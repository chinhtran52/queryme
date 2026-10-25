package app

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func editingTab(t *testing.T, text string, row, col int) (*App, *Tab, *fakeCopier) {
	t.Helper()
	a := connectedApp(t, nil)
	clipboard := &fakeCopier{}
	a.session.clipboard = clipboard
	tab := a.activeTab()
	tab.setLines(strings.Split(text, "\n"), row, col)
	return a, tab, clipboard
}

// runAll delivers the messages of a command (one level of batching), like the program loop would.
func runAll(a *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if msg := cmd(); msg != nil {
		send(a, msg)
	}
}

func cursorAt(tab *Tab) (int, int) {
	_, row, col := tab.cursor()
	return row, col
}

func TestCtrlDDuplicatesTheCurrentLine(t *testing.T) {
	a, tab, _ := editingTab(t, "SELECT id\nFROM users\nWHERE id = 1", 1, 4)
	send(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	if got := tab.editor.Value(); got != "SELECT id\nFROM users\nFROM users\nWHERE id = 1" {
		t.Fatalf("editor = %q", got)
	}
	if row, col := cursorAt(tab); row != 2 || col != 4 {
		t.Fatalf("cursor = %d,%d, want 2,4 (same column on the copy)", row, col)
	}
}

func TestCtrlXCutsTheLineToTheClipboard(t *testing.T) {
	a, tab, clipboard := editingTab(t, "SELECT id\nFROM users\nWHERE id = 1", 1, 3)
	runAll(a, send(a, tea.KeyMsg{Type: tea.KeyCtrlX}))
	if got := tab.editor.Value(); got != "SELECT id\nWHERE id = 1" {
		t.Fatalf("editor = %q", got)
	}
	if len(clipboard.copied) != 1 || clipboard.copied[0] != "FROM users\n" {
		t.Fatalf("clipboard = %q", clipboard.copied)
	}
	if row, col := cursorAt(tab); row != 1 || col != 0 || tab.notice != "Cut line 2 to the clipboard" {
		t.Fatalf("cursor = %d,%d notice = %q", row, col, tab.notice)
	}

	_, last, _ := editingTab(t, "only line", 0, 4)
	last.cutLine()
	if last.editor.Value() != "" {
		t.Fatalf("cutting the only line should leave it empty, got %q", last.editor.Value())
	}
	if last.cutLine() != nil {
		t.Fatal("cutting an empty editor copies nothing")
	}
}

func TestCtrlVPastesAtTheCursor(t *testing.T) {
	a, tab, clipboard := editingTab(t, "SELECT  FROM users", 0, 7)
	clipboard.clipboard = "id,\n  email"
	runAll(a, send(a, tea.KeyMsg{Type: tea.KeyCtrlV}))
	if got := tab.editor.Value(); got != "SELECT id,\n  email FROM users" {
		t.Fatalf("editor = %q", got)
	}
}

func TestCtrlVFallsBackToTheLastCut(t *testing.T) {
	a, tab, clipboard := editingTab(t, "SELECT 1\nSELECT 2", 1, 0)
	clipboard.pasteErr = errors.New("no clipboard tool found")

	runAll(a, send(a, tea.KeyMsg{Type: tea.KeyCtrlV}))
	if !strings.HasPrefix(tab.notice, "Nothing to paste") || tab.editor.Value() != "SELECT 1\nSELECT 2" {
		t.Fatalf("with nothing copied: notice=%q editor=%q", tab.notice, tab.editor.Value())
	}

	runAll(a, send(a, tea.KeyMsg{Type: tea.KeyCtrlX})) // cuts "SELECT 2"; the cursor goes to the start of "SELECT 1"
	runAll(a, send(a, tea.KeyMsg{Type: tea.KeyCtrlV}))
	if got := tab.editor.Value(); got != "SELECT 2\nSELECT 1" { // a pasted whole line lands above, as in most editors
		t.Fatalf("editor = %q", got)
	}
	if !strings.Contains(tab.notice, "queryme's last copy") {
		t.Fatalf("notice = %q", tab.notice)
	}
}

func TestLineShortcutsOnlyActInTheEditor(t *testing.T) {
	a, tab, _ := editingTab(t, "SELECT 1", 0, 0)
	press(a, "ctrl+k") // the AI prompt has focus
	send(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	send(a, tea.KeyMsg{Type: tea.KeyCtrlX})
	if tab.editor.Value() != "SELECT 1" {
		t.Fatalf("editor changed while it had no focus: %q", tab.editor.Value())
	}
}
