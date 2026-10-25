package app

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/db"
)

type fakeDriver struct{ res *db.Result }

func (f fakeDriver) Query(context.Context, string, int) (*db.Result, error) { return f.res, nil }
func (fakeDriver) Target() string                                           { return "fake" }
func (fakeDriver) Close()                                                   {}

type fakeCopier struct {
	copied    []string
	clipboard string // what Paste returns
	pasteErr  error
}

func (f *fakeCopier) Copy(text string) (string, error) {
	f.copied = append(f.copied, text)
	return "clipboard", nil
}

func ptr(s string) *string { return &s }

// newLoadedTab returns an 80x30 tab with a two-row result; rowY gives the screen row of a result row.
func newLoadedTab(t *testing.T) (*Tab, *fakeCopier) {
	t.Helper()
	res := &db.Result{
		Columns: []string{"id", "email", "profile"},
		Rows: [][]*string{
			{ptr("1"), ptr("ann@example.com"), ptr(`{"theme":"dark"}`)},
			{ptr("2"), ptr("bob@example.com"), nil},
		},
		Command: "SELECT 2",
	}
	copier := &fakeCopier{}
	tab := NewTab(1, &session{driver: fakeDriver{res: res}, clipboard: copier, ui: config.Default().UI})
	tab.SetSize(80, 30)
	tab.editor.SetValue("SELECT 1")
	tab.Update(tab.run()())
	return tab, copier
}

// rowY skips the table's header and separator lines.
func rowY(tab *Tab, row int) int { return tab.resultsTop() + 2 + row }

func click(tab *Tab, x, y int) tea.Cmd {
	return tab.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

func rightClick(tab *Tab, x, y int) tea.Cmd {
	return tab.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
}

func runCmd(tab *Tab, cmd tea.Cmd) {
	if cmd != nil {
		if msg := cmd(); msg != nil {
			tab.Update(msg)
		}
	}
}

func TestRightClickRowOpensMenuAndCopiesJSON(t *testing.T) {
	tab, copier := newLoadedTab(t)

	rightClick(tab, 10, rowY(tab, 1)) // second row, "email" column: gutter " 2 │" is x 0-3, "id" 4-8
	if tab.menu == nil || tab.focus != focusResults {
		t.Fatalf("menu=%v focus=%v", tab.menu, tab.focus)
	}
	if row, col := tab.results.Cursor(); row != 1 || col != 1 {
		t.Fatalf("cursor = %d,%d", row, col)
	}
	view := ansi.Strip(tab.View())
	if !strings.Contains(view, "Row 2") || !strings.Contains(view, "Copy row (JSON)") {
		t.Fatalf("menu not drawn:\n%s", view)
	}

	x0, y0 := tab.menuOrigin()
	runCmd(tab, click(tab, x0+3, tab.resultsTop()+y0+3)) // border, title, TSV, then JSON
	if tab.menu != nil || len(copier.copied) != 1 {
		t.Fatalf("menu=%v copied=%v", tab.menu, copier.copied)
	}
	if want := "{\n  \"id\": \"2\",\n  \"email\": \"bob@example.com\",\n  \"profile\": null\n}"; copier.copied[0] != want {
		t.Fatalf("copied %q", copier.copied[0])
	}
	if !strings.Contains(tab.status(), "Copied row 2 as JSON to the clipboard") {
		t.Fatalf("status = %q", tab.status())
	}
}

func TestTabAndShiftTabCycleFocusBothWays(t *testing.T) {
	tab, _ := newLoadedTab(t)
	forward := []focusArea{focusPrompt, focusResults, focusEditor}
	for i, want := range forward {
		tab.Update(tea.KeyMsg{Type: tea.KeyTab})
		if tab.focus != want {
			t.Fatalf("tab #%d: focus=%v want %v", i+1, tab.focus, want)
		}
	}
	backward := []focusArea{focusResults, focusPrompt, focusEditor}
	for i, want := range backward {
		tab.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
		if tab.focus != want {
			t.Fatalf("shift+tab #%d: focus=%v want %v", i+1, tab.focus, want)
		}
	}
	if !tab.editor.Focused() || tab.prompt.Focused() || tab.results.Focused() {
		t.Fatal("only the editor should hold focus after a full cycle")
	}

	tab.showNotes("✦ AI", "some notes")
	tab.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	tab.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if tab.focus != focusPrompt || tab.panel == nil {
		t.Fatalf("shift+tab should leave an open panel: focus=%v panel=%v", tab.focus, tab.panel != nil)
	}
}

func TestKeyboardCopyAndViewer(t *testing.T) {
	tab, copier := newLoadedTab(t)
	tab.Update(tea.KeyMsg{Type: tea.KeyTab}) // editor → AI prompt
	tab.Update(tea.KeyMsg{Type: tea.KeyTab}) // AI prompt → results
	tab.Update(tea.KeyMsg{Type: tea.KeyRight})

	runCmd(tab, tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}))
	runCmd(tab, tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")}))
	if strings.Join(copier.copied, "|") != "ann@example.com|1\tann@example.com\t{\"theme\":\"dark\"}" {
		t.Fatalf("copied %q", copier.copied)
	}

	tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for range 3 {
		tab.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if tab.panel == nil || !strings.Contains(ansi.Strip(tab.View()), `"theme": "dark"`) {
		t.Fatalf("viewer not shown:\n%s", ansi.Strip(tab.View()))
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tab.panel != nil || tab.focus != focusResults {
		t.Fatal("esc should close only the viewer")
	}
}

func TestClickOutsideMenuClosesItAndClickEditorFocusesIt(t *testing.T) {
	tab, copier := newLoadedTab(t)
	rightClick(tab, 6, rowY(tab, 0))
	click(tab, 0, tab.resultsTop()+tab.resultsHeight-1)
	if tab.menu != nil || len(copier.copied) != 0 {
		t.Fatalf("menu=%v copied=%v", tab.menu, copier.copied)
	}
	click(tab, 5, 2)
	if tab.focus != focusEditor {
		t.Fatal("clicking the editor should focus it")
	}
}

func TestViewKeepsItsHeightWithLongError(t *testing.T) {
	tab, _ := newLoadedTab(t)
	tab.Update(queryDoneMsg{err: &longError{}})
	if got := strings.Count(tab.View(), "\n") + 1; got != 30 {
		t.Fatalf("view is %d lines, want 30", got)
	}
	if !strings.Contains(ansi.Strip(tab.View()), "END") {
		t.Fatal("long error should wrap, not be cut")
	}
}

type longError struct{}

func (*longError) Error() string { return strings.Repeat("connection refused ", 10) + "END" }

func TestLeftClickSelectsAndFocusesWithoutMenu(t *testing.T) {
	tab, _ := newLoadedTab(t)
	click(tab, 10, rowY(tab, 1))
	if tab.menu != nil || tab.focus != focusResults {
		t.Fatalf("menu=%v focus=%v", tab.menu, tab.focus)
	}
	if row, col := tab.results.Cursor(); row != 1 || col != 1 {
		t.Fatalf("cursor = %d,%d", row, col)
	}

	rightClick(tab, 10, rowY(tab, 0))
	rightClick(tab, 5, rowY(tab, 1)) // a second right click moves the popup to the new row
	if tab.menu == nil {
		t.Fatal("right click should open the menu")
	}
	if row, col := tab.results.Cursor(); row != 1 || col != 0 {
		t.Fatalf("cursor after right click = %d,%d", row, col)
	}
}

func (fakeDriver) Schema(context.Context) (*db.Schema, error) {
	return &db.Schema{Version: "PostgreSQL 17", Tables: []db.Table{{Schema: "public", Name: "users"}}}, nil
}

func TestCtrlJFocusesTheEditorFromAnywhere(t *testing.T) {
	ctrlJ := tea.KeyMsg{Type: tea.KeyCtrlJ}
	tab, _ := newLoadedTab(t)

	tab.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	tab.Update(ctrlJ)
	if tab.focus != focusEditor || !tab.editor.Focused() || tab.prompt.Focused() {
		t.Fatalf("from the AI prompt: focus=%v", tab.focus)
	}

	rightClick(tab, 10, rowY(tab, 0)) // results focused with the row menu open
	tab.Update(ctrlJ)
	if tab.focus != focusEditor || tab.menu != nil || tab.results.Focused() {
		t.Fatalf("from the row menu: focus=%v menu=%v", tab.focus, tab.menu != nil)
	}

	tab.showNotes("✦ AI", "notes")
	tab.focusResults()
	tab.Update(ctrlJ)
	if tab.focus != focusEditor || tab.panel == nil {
		t.Fatalf("from a panel: focus=%v (the panel stays open behind)", tab.focus)
	}
	if strings.Contains(tab.editor.Value(), "\n") {
		t.Fatal("ctrl+j must not insert a newline")
	}
}

func (f *fakeCopier) Paste() (string, error) {
	if f.pasteErr != nil {
		return "", f.pasteErr
	}
	return f.clipboard, nil
}

func TestDoubleClickCopiesTheCell(t *testing.T) {
	tab, copier := newLoadedTab(t)
	x := 10 // the "email" column

	runCmd(tab, click(tab, x, rowY(tab, 1)))
	if len(copier.copied) != 0 || tab.menu != nil {
		t.Fatal("a single click only selects")
	}
	for _, msg := range runBatch(click(tab, x, rowY(tab, 1))) {
		tab.Update(msg)
	}
	if strings.Join(copier.copied, "|") != "bob@example.com" || !strings.Contains(tab.notice, "Copied email of row 2") {
		t.Fatalf("copied=%q notice=%q", copier.copied, tab.notice)
	}

	runBatch(click(tab, x, rowY(tab, 1))) // a quick third click must not copy again
	if len(copier.copied) != 1 {
		t.Fatalf("third click copied again: %q", copier.copied)
	}

	runCmd(tab, click(tab, x, rowY(tab, 0)))
	runCmd(tab, click(tab, x, rowY(tab, 1))) // two different cells
	tab.lastClick.at = time.Now().Add(-time.Second)
	runCmd(tab, click(tab, x, rowY(tab, 1))) // same cell, but too slow
	if len(copier.copied) != 1 {
		t.Fatalf("different cells or slow clicks must not copy: %q", copier.copied)
	}
}

// runBatch runs a command and, for a batch, each of its parts, returning their messages.
func runBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		msgs = append(msgs, runBatch(c)...)
	}
	return msgs
}
