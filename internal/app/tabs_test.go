package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/db"
	"github.com/chinhtran52/queryme/internal/history"
)

// blockingDriver answers "SELECT 1" at once and blocks anything else until its context is cancelled.
type blockingDriver struct{}

func (blockingDriver) Query(ctx context.Context, sql string, _ int) (*db.Result, error) {
	if sql == "SELECT 1" {
		one := "1"
		return &db.Result{Columns: []string{"n"}, Rows: [][]*string{{&one}}, Command: "SELECT 1"}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func (blockingDriver) Target() string { return "u@h:5432/db" }
func (blockingDriver) Close()         {}

func connectedApp(t *testing.T, queryLog *history.QueryLog) *App {
	t.Helper()
	a := New(config.Default(), emptyHistory(t), queryLog, nil)
	send(a, tea.WindowSizeMsg{Width: 100, Height: 30})
	send(a, connectedMsg{driver: blockingDriver{}})
	return a
}

func press(a *App, key string) tea.Cmd {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "ctrl+t":
		msg = tea.KeyMsg{Type: tea.KeyCtrlT}
	case "ctrl+w":
		msg = tea.KeyMsg{Type: tea.KeyCtrlW}
	case "ctrl+r":
		msg = tea.KeyMsg{Type: tea.KeyCtrlR}
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+pgdown":
		msg = tea.KeyMsg{Type: tea.KeyCtrlPgDown}
	case "alt+1":
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1"), Alt: true}
	}
	return send(a, msg)
}

func typeSQL(a *App, sql string) {
	send(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(sql)})
}

func TestOpenSwitchAndCloseTabs(t *testing.T) {
	a := connectedApp(t, nil)
	press(a, "ctrl+t")
	press(a, "ctrl+t")
	if len(a.tabs) != 3 || a.active != 2 {
		t.Fatalf("tabs=%d active=%d", len(a.tabs), a.active)
	}
	press(a, "alt+1")
	if a.active != 0 {
		t.Fatalf("alt+1 → active=%d", a.active)
	}
	press(a, "ctrl+pgdown")
	if a.active != 1 {
		t.Fatalf("ctrl+pgdown → active=%d", a.active)
	}
	for range 3 {
		press(a, "ctrl+w")
	}
	if len(a.tabs) != 1 || a.tabs[0].id != 4 {
		t.Fatalf("closing every tab should leave one fresh tab, got %d tabs (id %d)", len(a.tabs), a.tabs[0].id)
	}
}

func TestResultReachesTheTabThatRanIt(t *testing.T) {
	a := connectedApp(t, nil)
	typeSQL(a, "SELECT 1")
	run := press(a, "ctrl+r")
	press(a, "ctrl+t")
	send(a, run())

	first, second := a.tabs[0], a.tabs[1]
	if first.result == nil || second.result != nil || a.active != 1 {
		t.Fatalf("first=%v second=%v active=%d", first.result, second.result, a.active)
	}
	view := ansi.Strip(a.View())
	if !strings.Contains(view, " 1 SELECT 1 ") || !strings.Contains(view, " 2 Query 2 ") {
		t.Fatalf("tab bar:\n%s", strings.SplitN(view, "\n", 3)[1])
	}
}

func TestCtrlCCancelsRunningQueryThenQuits(t *testing.T) {
	a := connectedApp(t, nil)
	typeSQL(a, "SELECT pg_sleep(60)")
	run := press(a, "ctrl+r")
	if !strings.Contains(ansi.Strip(a.tabBar()), "●") {
		t.Fatalf("running tab should be marked: %q", ansi.Strip(a.tabBar()))
	}

	done := make(chan tea.Msg, 1)
	go func() { done <- run() }()
	if cmd := press(a, "ctrl+c"); cmd != nil {
		t.Fatal("ctrl+c with a running query must cancel, not quit")
	}
	select {
	case msg := <-done:
		send(a, msg)
	case <-time.After(2 * time.Second):
		t.Fatal("query was not cancelled")
	}
	if a.activeTab().err != errCancelled || !strings.Contains(ansi.Strip(a.View()), "query cancelled") {
		t.Fatalf("err=%v", a.activeTab().err)
	}

	if cmd := press(a, "ctrl+c"); cmd == nil || cmd() != tea.Quit() {
		t.Fatal("ctrl+c when idle should quit")
	}
}

func TestClosingATabCancelsItsQuery(t *testing.T) {
	a := connectedApp(t, nil)
	typeSQL(a, "SELECT pg_sleep(60)")
	run := press(a, "ctrl+r")
	done := make(chan tea.Msg, 1)
	go func() { done <- run() }()
	press(a, "ctrl+w")
	select {
	case msg := <-done:
		send(a, msg) // the tab is gone; this must be ignored, not panic
	case <-time.After(2 * time.Second):
		t.Fatal("closing the tab did not cancel its query")
	}
}

func TestTabBarClicks(t *testing.T) {
	a := connectedApp(t, nil)
	items := a.tabBarItems()
	plus := items[len(items)-1]
	send(a, tea.MouseMsg{X: plus.x0 + 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if len(a.tabs) != 2 || a.active != 1 {
		t.Fatalf("clicking + : tabs=%d active=%d", len(a.tabs), a.active)
	}
	send(a, tea.MouseMsg{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if a.active != 0 {
		t.Fatalf("clicking tab 1: active=%d", a.active)
	}
}

func TestQueriesAreLogged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queries.jsonl")
	a := connectedApp(t, history.NewQueryLog(path))
	typeSQL(a, "SELECT 1")
	send(a, press(a, "ctrl+r")())

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"sql":"SELECT 1"`, `"connection":"u@h:5432/db"`, `"rows":1`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("log missing %s: %s", want, data)
		}
	}
}

func TestCancelAgainstPostgres(t *testing.T) {
	url := os.Getenv("QUERYME_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("QUERYME_TEST_DATABASE_URL not set")
	}
	a := New(config.Default(), emptyHistory(t), nil, &config.Connection{URL: url})
	defer a.Close()
	send(a, tea.WindowSizeMsg{Width: 100, Height: 30})
	send(a, a.Init()())

	typeSQL(a, "SELECT pg_sleep(30)")
	run := press(a, "ctrl+r")
	done := make(chan tea.Msg, 1)
	start := time.Now()
	go func() { done <- run() }()
	time.Sleep(200 * time.Millisecond)
	press(a, "ctrl+c")
	select {
	case msg := <-done:
		send(a, msg)
	case <-time.After(5 * time.Second):
		t.Fatal("Postgres query was not cancelled")
	}
	if a.activeTab().err != errCancelled || time.Since(start) > 5*time.Second {
		t.Fatalf("err=%v after %v", a.activeTab().err, time.Since(start))
	}

	a.activeTab().editor.SetValue("SELECT 'still usable' AS ok")
	send(a, press(a, "ctrl+r")())
	if a.activeTab().err != nil {
		t.Fatalf("connection unusable after cancel: %v", a.activeTab().err)
	}
}

func (blockingDriver) Schema(context.Context) (*db.Schema, error) {
	return &db.Schema{Version: "PostgreSQL 17", Tables: []db.Table{{Schema: "public", Name: "users"}}}, nil
}

func TestHelpOverlayOpensWithF1AndQuestionMark(t *testing.T) {
	a := connectedApp(t, nil)
	send(a, tea.KeyMsg{Type: tea.KeyF1})
	if !a.showHelp || !strings.Contains(ansi.Strip(a.View()), "queryme keys") {
		t.Fatal("F1 should show the help")
	}
	typeSQL(a, "x")
	if a.showHelp || a.activeTab().editor.Value() != "" {
		t.Fatal("any key closes the help and is not typed into the editor")
	}

	typeSQL(a, "SELECT 1")
	send(a, press(a, "ctrl+r")())
	send(a, tea.KeyMsg{Type: tea.KeyShiftTab}) // editor → results
	cmd := send(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if cmd == nil {
		t.Fatal("? in the results should ask for help")
	}
	send(a, cmd())
	if !a.showHelp {
		t.Fatal("? in the results should show the help")
	}
	send(a, tea.MouseMsg{X: 1, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if a.showHelp {
		t.Fatal("a click closes the help")
	}
}
