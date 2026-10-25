package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/chinhtran52/queryme/internal/ai"
	"github.com/chinhtran52/queryme/internal/clip"
	"github.com/chinhtran52/queryme/internal/complete"
	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/db"
	"github.com/chinhtran52/queryme/internal/db/postgres"
	"github.com/chinhtran52/queryme/internal/history"
	"github.com/chinhtran52/queryme/internal/ui"
)

const (
	connectTimeout = 10 * time.Second
	chromeHeight   = 2 // header line + tab bar
	newTabIndex    = -1
)

type connectedMsg struct {
	conn   config.Connection
	driver db.Driver
}
type connectFailedMsg struct{ err error }

type App struct {
	cfg        config.Config
	history    *history.Store
	queryLog   *history.QueryLog
	claudeDir  string
	clipboard  Clipboard
	start      *config.Connection
	picker     *ui.Picker
	session    *session
	tabs       []*Tab
	active     int
	nextID     int
	connecting string
	showHelp   bool
	width      int
	height     int
}

// New skips the picker when start is set. queryLog may be nil to keep no query history.
func New(cfg config.Config, hist *history.Store, queryLog *history.QueryLog, start *config.Connection) *App {
	// Without a data dir the AI reports an error on use instead of running claude somewhere shared.
	claudeDir := ""
	if dir, err := history.DefaultDir(); err == nil {
		claudeDir = filepath.Join(dir, "claude")
	}
	return &App{
		cfg: cfg, history: hist, queryLog: queryLog, claudeDir: claudeDir, clipboard: clip.New(), start: start,
		picker: ui.NewPicker(hist.Entries(), cfg.Candidates(os.Getenv), hist.SavesPasswords()),
	}
}

func (a *App) Init() tea.Cmd {
	if a.start != nil {
		return a.connect(*a.start)
	}
	return nil
}

func (a *App) Close() {
	for _, t := range a.tabs {
		t.Cancel()
	}
	if a.session != nil {
		a.session.driver.Close()
	}
}

func (a *App) connect(c config.Connection) tea.Cmd {
	a.connecting = c.Name
	if a.connecting == "" {
		a.connecting = history.Redact(c.URL)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()
		driver, err := postgres.Open(ctx, c.URL, c.Password, c.SSH, c.ReadOnly)
		if err != nil {
			return connectFailedMsg{err: err}
		}
		return connectedMsg{conn: c, driver: driver}
	}
}

func (a *App) activeTab() *Tab { return a.tabs[a.active] }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		for _, t := range a.tabs {
			t.SetSize(a.width, a.height-chromeHeight)
		}
		return a, nil
	case tea.MouseMsg:
		return a, a.handleMouse(msg)
	case tea.KeyMsg:
		if cmd, handled := a.handleKey(msg); handled {
			return a, cmd
		}
	case ui.PickedMsg:
		return a, a.connect(msg.Connection)
	case connectedMsg:
		a.connecting = ""
		schema := ai.NewSchemaCache(msg.driver.Schema)
		a.session = &session{
			driver: msg.driver, clipboard: a.clipboard, queryLog: a.queryLog, ui: a.cfg.UI,
			ai:     ai.NewAssistant(a.cfg.AI.ClaudeBin, a.claudeDir, schema),
			schema: schema, completer: complete.New(nil),
		}
		// History is a convenience; failing to write it must not block the session.
		_ = a.history.Record(msg.conn, time.Now())
		return a, tea.Batch(a.openTab("", ""), loadSchema(schema))
	case schemaLoadedMsg:
		if msg.err != nil {
			a.activeTab().notice = "Autocomplete has keywords only: " + msg.err.Error()
			return a, nil
		}
		a.session.completer = complete.New(msg.schema)
		return a, nil
	case ui.RenameMsg:
		if _, err := a.history.Rename(msg.URL, msg.SSH, msg.Name); err != nil {
			a.picker.SetError(err)
		}
		a.picker.SetRecent(a.history.Entries())
		return a, nil
	case ui.ForgetMsg:
		if _, err := a.history.Delete(msg.URL, msg.SSH); err != nil {
			a.picker.SetError(err)
		}
		a.picker.SetRecent(a.history.Entries())
		return a, nil
	case connectFailedMsg:
		a.connecting = ""
		a.picker.SetError(msg.err)
		return a, nil
	case showHelpMsg:
		a.showHelp = true
		return a, nil
	case openTabMsg:
		cmd := a.openTab(msg.sql, msg.title)
		t := a.activeTab()
		t.aiSession = msg.aiSession
		t.notice = "AI wrote the query in a new tab: review it, then ctrl+r to run"
		if msg.notes != "" {
			t.showNotes("✦ AI", msg.notes)
		}
		return a, cmd
	case tabMsg:
		if t := a.tabByID(msg.tabID()); t != nil {
			return a, t.Update(msg)
		}
		return a, nil
	}

	switch {
	case a.session != nil:
		return a, a.activeTab().Update(msg)
	case a.connecting != "":
		return a, nil
	}
	return a, a.picker.Update(msg)
}

// handleKey takes the app-wide keys; handled is false for keys that belong to the active tab or picker.
func (a *App) handleKey(key tea.KeyMsg) (cmd tea.Cmd, handled bool) {
	s := key.String()
	switch {
	case s == "ctrl+q":
		return tea.Quit, true
	case a.showHelp:
		a.showHelp = false
		return nil, true
	case s == "f1" && a.session != nil:
		a.showHelp = true
		return nil, true
	}
	switch s {
	case "ctrl+c":
		if a.session != nil && a.activeTab().Running() {
			a.activeTab().Cancel()
			return nil, true
		}
		return tea.Quit, true
	}
	if a.session == nil {
		return nil, false
	}
	switch s {
	case "ctrl+t":
		return a.openTab("", ""), true
	case "ctrl+w":
		return a.closeTab(a.active), true
	case "ctrl+pgdown":
		return a.selectTab((a.active + 1) % len(a.tabs)), true
	case "ctrl+pgup":
		return a.selectTab((a.active + len(a.tabs) - 1) % len(a.tabs)), true
	}
	if n, ok := strings.CutPrefix(s, "alt+"); ok && len(n) == 1 && n[0] >= '1' && n[0] <= '9' {
		return a.selectTab(int(n[0] - '1')), true
	}
	return nil, false
}

func (a *App) handleMouse(m tea.MouseMsg) tea.Cmd {
	if a.session == nil {
		return nil
	}
	if a.showHelp {
		if m.Action == tea.MouseActionPress {
			a.showHelp = false
		}
		return nil
	}
	if m.Y == 1 {
		if m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft {
			return a.clickTabBar(m.X)
		}
		return nil
	}
	if m.Y < chromeHeight {
		return nil
	}
	m.Y -= chromeHeight
	return a.activeTab().Update(m)
}

func (a *App) tabByID(id int) *Tab {
	for _, t := range a.tabs {
		if t.id == id {
			return t
		}
	}
	return nil
}

// openTab adds a tab after the others and switches to it.
func (a *App) openTab(sql, title string) tea.Cmd {
	a.nextID++
	t := NewTab(a.nextID, a.session)
	t.title = title
	t.editor.SetValue(sql)
	t.SetSize(a.width, a.height-chromeHeight)
	a.tabs = append(a.tabs, t)
	a.active = len(a.tabs) - 1
	return t.Init()
}

// closeTab cancels the tab's query; closing the last tab leaves a fresh empty one.
func (a *App) closeTab(i int) tea.Cmd {
	a.tabs[i].Cancel()
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	if len(a.tabs) == 0 {
		return a.openTab("", "")
	}
	a.active = min(a.active, len(a.tabs)-1)
	return nil
}

func (a *App) selectTab(i int) tea.Cmd {
	if i >= 0 && i < len(a.tabs) {
		a.active = i
	}
	return nil
}

type tabBarItem struct {
	label  string
	x0, x1 int
	index  int
}

// tabBarItems lays out the tab bar once, for both drawing and mouse hit-testing.
func (a *App) tabBarItems() []tabBarItem {
	var items []tabBarItem
	x := 0
	add := func(label string, index int) {
		w := runewidth.StringWidth(label)
		items = append(items, tabBarItem{label: label, x0: x, x1: x + w, index: index})
		x += w + 1
	}
	for i, t := range a.tabs {
		label := fmt.Sprintf(" %d %s ", i+1, t.Title())
		if t.Running() {
			label = fmt.Sprintf(" %d %s ● ", i+1, t.Title())
		}
		add(label, i)
	}
	add(" + ", newTabIndex)
	return items
}

func (a *App) clickTabBar(x int) tea.Cmd {
	for _, item := range a.tabBarItems() {
		if x >= item.x0 && x < item.x1 {
			if item.index == newTabIndex {
				return a.openTab("", "")
			}
			return a.selectTab(item.index)
		}
	}
	return nil
}

func (a *App) tabBar() string {
	parts := make([]string, 0, len(a.tabs)+1)
	for _, item := range a.tabBarItems() {
		style := ui.DimStyle
		if item.index == a.active {
			style = ui.HeaderStyle.Padding(0)
		}
		parts = append(parts, style.Render(item.label))
	}
	// shortcut: tabs past the right edge are cut off; scroll the bar when people keep many tabs open.
	return ansi.Truncate(strings.Join(parts, " "), a.width, "…")
}

func (a *App) View() string {
	switch {
	case a.session != nil:
		header := ui.HeaderStyle.Render("queryme · " + a.session.driver.Target())
		screen := header + "\n" + a.tabBar() + "\n" + a.activeTab().View()
		if a.showHelp {
			help := ui.HelpView()
			x, y := max((a.width-lipgloss.Width(help))/2, 0), max((a.height-lipgloss.Height(help))/2, 0)
			screen = ui.Overlay(screen, help, x, y)
		}
		return screen
	case a.connecting != "":
		return ui.HeaderStyle.Render("queryme") + "\n\nConnecting to " + a.connecting + "…"
	}
	return a.picker.View()
}
