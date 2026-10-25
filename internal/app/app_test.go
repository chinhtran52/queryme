package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/history"
)

func send(a *App, msg tea.Msg) tea.Cmd {
	_, cmd := a.Update(msg)
	return cmd
}

func emptyHistory(t *testing.T) *history.Store {
	t.Helper()
	s, err := history.Open(filepath.Join(t.TempDir(), "connections.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTypeRunAndRenderQuery(t *testing.T) {
	url := os.Getenv("QUERYME_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("QUERYME_TEST_DATABASE_URL not set")
	}
	hist := emptyHistory(t)
	a := New(config.Default(), hist, nil, &config.Connection{Name: "test", URL: url})
	defer a.Close()
	send(a, tea.WindowSizeMsg{Width: 100, Height: 30})
	send(a, a.Init()())

	if got := hist.Entries(); len(got) != 1 || strings.Contains(got[0].URL, "queryme") || !got[0].NeedsPassword {
		t.Fatalf("history after connect = %+v", got)
	}

	send(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("SELECT 'hello' AS greeting, NULL AS n")})
	run := send(a, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !strings.Contains(a.View(), "running…") {
		t.Fatalf("expected running status:\n%s", a.View())
	}
	send(a, run())

	view := a.View()
	for _, want := range []string{"postgres@localhost", "greeting", "hello", "NULL", "1 rows"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}

	firstRowY := chromeHeight + a.activeTab().resultsTop() + 2 // header and tab bar, then the table's header and separator
	send(a, tea.MouseMsg{X: 6, Y: firstRowY, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	if a.activeTab().menu == nil {
		t.Fatalf("right-clicking the first row should open the row menu:\n%s", a.View())
	}
	t.Log("\n" + a.View())
}

func TestConnectFailureReturnsToPickerWithError(t *testing.T) {
	for _, k := range []string{"DATABASE_URL", "PGHOST", "PGDATABASE", "PGSERVICE"} {
		t.Setenv(k, "")
	}
	hist := emptyHistory(t)
	a := New(config.Default(), hist, nil, nil)
	send(a, tea.WindowSizeMsg{Width: 100, Height: 30})
	send(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("postgres://nobody@127.0.0.1:1/x")})
	send(a, tea.KeyMsg{Type: tea.KeyEnter})
	send(a, tea.KeyMsg{Type: tea.KeyEnter})
	send(a, tea.KeyMsg{Type: tea.KeyEnter})
	pick := send(a, tea.KeyMsg{Type: tea.KeyEnter})
	connect := send(a, pick())
	send(a, connect())
	if view := a.View(); !strings.Contains(view, "Connection URL") || !strings.Contains(view, "failed to connect") {
		t.Fatalf("view:\n%s", view)
	}
	if len(hist.Entries()) != 0 {
		t.Fatalf("failed connection was remembered: %+v", hist.Entries())
	}
}

func TestForgetRemovesRecentConnection(t *testing.T) {
	for _, k := range []string{"DATABASE_URL", "PGHOST", "PGDATABASE", "PGSERVICE"} {
		t.Setenv(k, "")
	}
	hist := emptyHistory(t)
	for _, url := range []string{"postgres://a@h/one", "postgres://a@h/two"} {
		if err := hist.Record(config.Connection{URL: url}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	a := New(config.Default(), hist, nil, nil)
	send(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	forget := send(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	send(a, forget())

	if got := hist.Entries(); len(got) != 1 || got[0].URL != "postgres://a@h/one" {
		t.Fatalf("history = %+v", got)
	}
	if view := a.View(); strings.Contains(view, "/two") {
		t.Fatalf("picker still lists the forgotten connection:\n%s", view)
	}
}

func TestRenameRecentConnection(t *testing.T) {
	for _, k := range []string{"DATABASE_URL", "PGHOST", "PGDATABASE", "PGSERVICE"} {
		t.Setenv(k, "")
	}
	hist := emptyHistory(t)
	if err := hist.Record(config.Connection{URL: "postgres://a@h/one"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	a := New(config.Default(), hist, nil, nil)
	send(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	send(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("staging")})
	rename := send(a, tea.KeyMsg{Type: tea.KeyEnter})
	send(a, rename())

	if got := hist.Entries()[0].Name; got != "staging" {
		t.Fatalf("name = %q", got)
	}
	if view := a.View(); !strings.Contains(view, "▶ staging") {
		t.Fatalf("picker:\n%s", view)
	}
}
