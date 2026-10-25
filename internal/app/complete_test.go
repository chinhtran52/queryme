package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinhtran52/queryme/internal/db"
)

var shopSchema = &db.Schema{Tables: []db.Table{
	{Schema: "public", Name: "orders", Columns: []db.Column{{Name: "id", Type: "bigint"}, {Name: "total", Type: "numeric"}, {Name: "ordered_at", Type: "timestamptz"}}},
	{Schema: "public", Name: "organizations", Columns: []db.Column{{Name: "name", Type: "text"}}},
	{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id", Type: "bigint"}, {Name: "email", Type: "text"}}},
}}

func completingApp(t *testing.T) *App {
	t.Helper()
	a := connectedApp(t, nil)
	send(a, schemaLoadedMsg{schema: shopSchema})
	return a
}

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func TestTabCompletesTableAfterFrom(t *testing.T) {
	a := completingApp(t)
	typeSQL(a, "SELECT * FROM or")
	tab := a.activeTab()
	if tab.suggest == nil {
		t.Fatal("typing a table prefix should open suggestions")
	}
	view := ansi.Strip(a.View())
	if !strings.Contains(view, "orders") || !strings.Contains(view, "organizations") || !strings.Contains(view, "table · public") {
		t.Fatalf("popup not drawn:\n%s", view)
	}

	send(a, key(tea.KeyDown))
	send(a, key(tea.KeyTab))
	if got := tab.editor.Value(); got != "SELECT * FROM organizations" {
		t.Fatalf("editor = %q", got)
	}
	if tab.suggest != nil || tab.focus != focusEditor {
		t.Fatalf("accepting should close the list and keep focus, focus=%v", tab.focus)
	}

	send(a, key(tea.KeyTab))
	if tab.focus != focusPrompt {
		t.Fatal("with no list open, tab moves focus as before")
	}
}

func TestTabCompletesAliasColumnsMidQuery(t *testing.T) {
	a := completingApp(t)
	tab := a.activeTab()
	tab.editor.SetValue("SELECT u.em FROM users u")
	tab.editor.SetCursor(0)
	for range 11 { // put the cursor after "u.em" on the first line
		send(a, key(tea.KeyRight))
	}
	typeSQL(a, "a") // "u.ema"
	if tab.suggest == nil {
		t.Fatalf("no suggestions for %q", tab.editor.Value())
	}
	send(a, key(tea.KeyTab))
	if got := tab.editor.Value(); got != "SELECT u.email FROM users u" {
		t.Fatalf("editor = %q", got)
	}
}

func TestSuggestionsCloseOnEscCursorMoveAndFocusChange(t *testing.T) {
	a := completingApp(t)
	tab := a.activeTab()

	typeSQL(a, "SELECT to")
	send(a, key(tea.KeyEsc))
	if tab.suggest != nil || tab.focus != focusEditor {
		t.Fatal("esc should only close the list")
	}

	typeSQL(a, "t")
	send(a, key(tea.KeyLeft))
	if tab.suggest != nil {
		t.Fatal("moving the cursor without typing should close the list")
	}

	typeSQL(a, "a")
	send(a, key(tea.KeyShiftTab))
	if tab.suggest != nil {
		t.Fatal("leaving the editor should close the list")
	}
}

func TestKeywordsCompleteBeforeTheSchemaLoads(t *testing.T) {
	a := connectedApp(t, nil) // no schemaLoadedMsg yet
	typeSQL(a, "sel")
	send(a, key(tea.KeyTab))
	if got := a.activeTab().editor.Value(); got != "select" {
		t.Fatalf("editor = %q", got)
	}
}
