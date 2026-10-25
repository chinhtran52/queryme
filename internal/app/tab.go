package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/chinhtran52/queryme/internal/db"
	"github.com/chinhtran52/queryme/internal/history"
	"github.com/chinhtran52/queryme/internal/ui"
)

const (
	wheelStep         = 3
	doubleClickWindow = 400 * time.Millisecond
)

type cellClick struct {
	row, col int
	at       time.Time
}

type focusArea int

const (
	focusEditor focusArea = iota
	focusPrompt
	focusResults
)

// Clipboard is the system clipboard; Paste may fail when no clipboard tool is available.
type Clipboard interface {
	Copy(text string) (how string, err error)
	Paste() (string, error)
}

type queryDoneMsg struct {
	tab     int
	sql     string
	res     *db.Result
	err     error
	elapsed time.Duration
}

type copyDoneMsg struct {
	tab             int
	verb, what, how string
	err             error
}

var errCancelled = errors.New("query cancelled")

// showHelpMsg asks the app to draw the help overlay.
type showHelpMsg struct{}

type Tab struct {
	id            int
	session       *session
	title         string
	cancel        context.CancelFunc
	editor        textarea.Model
	prompt        textarea.Model
	results       *ui.ResultTable
	menu          *ui.RowMenu
	panel         *ui.TextPanel
	suggest       *ui.SuggestList
	suggestWord   string
	lastClick     cellClick
	focus         focusArea
	running       bool
	asking        bool
	aiCancel      context.CancelFunc
	aiSession     string
	aiFrame       int
	result        *db.Result
	err           error
	notice        string
	width         int
	editorHeight  int
	resultsHeight int
}

func NewTab(id int, s *session) *Tab {
	editor := textarea.New()
	editor.Placeholder = ui.KeyLabels("SELECT …   (ctrl+r to run)")
	editor.ShowLineNumbers = true
	editor.CharLimit = 0
	editor.Focus()
	return &Tab{id: id, session: s, editor: editor, prompt: newPrompt(), results: ui.NewResultTable(s.ui.MaxColWidth)}
}

const maxTitleWidth = 20

// Title is the AI request that opened the tab, else the start of its SQL.
func (t *Tab) Title() string {
	title := t.title
	if title == "" {
		title = strings.Join(strings.Fields(t.editor.Value()), " ")
	}
	if title == "" {
		title = fmt.Sprintf("Query %d", t.id)
	}
	return runewidth.Truncate(title, maxTitleWidth, "…")
}

// Running is true while a query or an AI request is in flight.
func (t *Tab) Running() bool { return t.running || t.asking }

// Cancel stops the running query and AI request, if any; each then returns a context error.
func (t *Tab) Cancel() {
	for _, cancel := range []context.CancelFunc{t.cancel, t.aiCancel} {
		if cancel != nil {
			cancel()
		}
	}
}

func (t *Tab) Init() tea.Cmd { return textarea.Blink }

func (t *Tab) SetSize(width, height int) {
	t.width = width
	t.editorHeight = max(3, height/3)
	// The AI prompt area, the divider and the status line sit between the editor and the results.
	t.resultsHeight = max(1, height-t.editorHeight-promptLines-2)
	t.editor.SetWidth(width)
	t.editor.SetHeight(t.editorHeight)
	t.prompt.SetWidth(width)
	t.prompt.SetHeight(promptBoxLines)
	t.results.SetSize(width, t.resultsHeight)
	if t.panel != nil {
		t.panel.SetSize(width, t.resultsHeight)
	}
}

// editorGutter is the width of the textarea's prompt and line numbers, which it formats as " %*d ".
func (t *Tab) editorGutter() int {
	return lipgloss.Width(t.editor.Prompt) + len(strconv.Itoa(t.editor.MaxHeight)) + 2
}

func (t *Tab) inPrompt(y int) bool { return y >= t.editorHeight && y < t.editorHeight+promptLines }
func (t *Tab) resultsTop() int     { return t.editorHeight + promptLines + 1 }

func (t *Tab) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case queryDoneMsg:
		t.cancel = nil
		if errors.Is(msg.err, context.Canceled) {
			msg.err = errCancelled
		}
		t.running, t.result, t.err, t.menu, t.panel = false, msg.res, msg.err, nil, nil
		if msg.err == nil {
			t.results.SetResult(msg.res)
		}
		t.log(msg)
		return nil
	case aiDoneMsg:
		return t.handleAIDone(msg)
	case aiTickMsg:
		if !t.asking {
			return nil
		}
		t.aiFrame++
		return aiTick(t.id)
	case pasteDoneMsg:
		t.handlePaste(msg)
		return nil
	case copyDoneMsg:
		t.notice = msg.verb + " " + msg.what + " to the " + msg.how
		if msg.err != nil {
			t.notice = "Copy failed: " + msg.err.Error()
		}
		return nil
	case tea.MouseMsg:
		return t.handleMouse(msg)
	case tea.KeyMsg:
		return t.handleKey(msg)
	}
	var cmds [2]tea.Cmd
	t.editor, cmds[0] = t.editor.Update(msg)
	t.prompt, cmds[1] = t.prompt.Update(msg)
	return tea.Batch(cmds[:]...)
}

func (t *Tab) handleKey(key tea.KeyMsg) tea.Cmd {
	if key.String() == "ctrl+j" {
		t.menu = nil
		return t.focusEditor()
	}
	switch {
	case t.panel != nil && t.focus == focusResults && key.String() != "tab" && key.String() != "shift+tab":
		if !t.panel.Update(key) {
			t.panel = nil
		}
		return nil
	case t.menu != nil:
		return t.apply(t.menu.HandleKey(key))
	}
	if t.focus == focusEditor && t.suggest != nil && t.handleSuggestKey(key) {
		return nil
	}
	switch key.String() {
	case "ctrl+r", "f5":
		return t.run()
	case "ctrl+k":
		return t.focusPrompt()
	case "tab":
		return t.cycleFocus(1)
	case "shift+tab":
		return t.cycleFocus(-1)
	}
	switch t.focus {
	case focusEditor:
		if cmd, handled := t.editorShortcut(key); handled {
			return cmd
		}
		before := t.editor.Value()
		var cmd tea.Cmd
		t.editor, cmd = t.editor.Update(key)
		if t.editor.Value() != before {
			t.refreshSuggestions()
		} else {
			t.suggest = nil // the cursor moved without typing
		}
		return cmd
	case focusPrompt:
		return t.updatePrompt(key)
	}
	switch key.String() {
	case "enter":
		t.openMenu()
	case "y":
		return t.apply(ui.ActionCopyCell)
	case "Y":
		return t.apply(ui.ActionCopyRowTSV)
	case "?":
		return func() tea.Msg { return showHelpMsg{} }
	case "esc":
		return t.focusEditor()
	default:
		t.results.Update(key)
	}
	return nil
}

func (t *Tab) handleMouse(m tea.MouseMsg) tea.Cmd {
	if m.Action != tea.MouseActionPress {
		return nil
	}
	y := m.Y - t.resultsTop()
	if t.panel != nil && y >= 0 && m.Button != tea.MouseButtonLeft && m.Button != tea.MouseButtonRight {
		t.panel.Update(m)
		return nil
	}
	switch m.Button {
	case tea.MouseButtonWheelUp:
		t.results.MoveBy(-wheelStep, 0)
	case tea.MouseButtonWheelDown:
		t.results.MoveBy(wheelStep, 0)
	case tea.MouseButtonLeft:
		if t.menu != nil {
			x0, y0 := t.menuOrigin()
			return t.apply(t.menu.HandleClick(m.X-x0, y-y0))
		}
		return t.clickAt(m, y, false)
	case tea.MouseButtonRight:
		t.menu = nil
		return t.clickAt(m, y, true)
	}
	return nil
}

// clickAt focuses what was clicked; a click on a cell selects it, and openMenu also opens the row popup.
func (t *Tab) clickAt(m tea.MouseMsg, resultsY int, openMenu bool) tea.Cmd {
	switch {
	case m.Y < t.editorHeight:
		return t.focusEditor()
	case t.inPrompt(m.Y):
		return t.focusPrompt()
	case resultsY < 0:
		return nil
	}
	cmd := t.focusResults()
	if t.panel != nil {
		return cmd
	}
	if row, col, ok := t.results.HitTest(m.X, resultsY); ok {
		t.results.Select(row, col)
		if openMenu {
			t.openMenu()
		} else if t.isDoubleClick(row, col) {
			return tea.Batch(cmd, t.apply(ui.ActionCopyCell))
		}
	}
	return cmd
}

// isDoubleClick records a left click on a cell and reports whether it completes a double click there.
// The terminal only reports single presses, so two on the same cell within doubleClickWindow count as one.
func (t *Tab) isDoubleClick(row, col int) bool {
	previous := t.lastClick
	t.lastClick = cellClick{row: row, col: col, at: time.Now()}
	if previous.row == row && previous.col == col && time.Since(previous.at) < doubleClickWindow {
		t.lastClick = cellClick{} // a third quick click starts over instead of copying again
		return true
	}
	return false
}

// cycleFocus moves through editor → AI prompt → results, wrapping around; step -1 goes backwards.
func (t *Tab) cycleFocus(step int) tea.Cmd {
	const areas = 3 // focusEditor, focusPrompt, focusResults, in that order
	switch focusArea((int(t.focus) + step + areas) % areas) {
	case focusPrompt:
		return t.focusPrompt()
	case focusResults:
		return t.focusResults()
	}
	return t.focusEditor()
}

func (t *Tab) focusEditor() tea.Cmd {
	t.focus = focusEditor
	t.results.Blur()
	t.prompt.Blur()
	return t.editor.Focus()
}

func (t *Tab) focusResults() tea.Cmd {
	t.focus, t.suggest = focusResults, nil
	t.editor.Blur()
	t.prompt.Blur()
	t.results.Focus()
	return nil
}

func (t *Tab) openMenu() {
	if t.results.HasRows() {
		row, _ := t.results.Cursor()
		t.menu = ui.NewRowMenu(row)
	}
}

// menuOrigin centres the row menu in the results pane.
func (t *Tab) menuOrigin() (int, int) {
	w, h := t.menu.Size()
	return max((t.width-w)/2, 0), max((t.resultsHeight-h)/2, 0)
}

func (t *Tab) apply(action ui.RowAction) tea.Cmd {
	if action == ui.ActionNone {
		return nil
	}
	t.menu = nil
	if !t.results.HasRows() {
		return nil
	}
	row, col := t.results.Cursor()
	switch action {
	case ui.ActionCopyRowTSV:
		return t.copy(fmt.Sprintf("row %d as TSV", row+1), t.result.RowTSV(row))
	case ui.ActionCopyRowJSON:
		return t.copy(fmt.Sprintf("row %d as JSON", row+1), t.result.RowJSON(row))
	case ui.ActionCopyCell:
		return t.copy(fmt.Sprintf("%s of row %d", t.result.Columns[col], row+1), t.result.Cell(row, col))
	case ui.ActionViewRow:
		t.panel = ui.NewRowViewer(t.result, row, t.width, t.resultsHeight)
	}
	return nil
}

func (t *Tab) copy(what, text string) tea.Cmd { return t.copyAs("Copied", what, text) }

// copyAs puts text on the clipboard and remembers it, so paste works even without a clipboard tool.
func (t *Tab) copyAs(verb, what, text string) tea.Cmd {
	t.session.lastCopied = text
	clipboard, id := t.session.clipboard, t.id
	return func() tea.Msg {
		how, err := clipboard.Copy(text)
		return copyDoneMsg{tab: id, verb: verb, what: what, how: how, err: err}
	}
}

func (t *Tab) run() tea.Cmd {
	sql := strings.TrimSpace(t.editor.Value())
	if sql == "" || t.running {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.running, t.notice, t.cancel = true, "", cancel
	driver, limit, id := t.session.driver, t.session.ui.RowLimit, t.id
	return func() tea.Msg {
		start := time.Now()
		res, err := driver.Query(ctx, sql, limit)
		return queryDoneMsg{tab: id, sql: sql, res: res, err: err, elapsed: time.Since(start)}
	}
}

// log records the query; the log is a convenience, so a failed write is ignored.
func (t *Tab) log(msg queryDoneMsg) {
	if t.session.queryLog == nil {
		return
	}
	entry := history.QueryLogEntry{
		Time:       time.Now().Add(-msg.elapsed),
		Connection: t.session.driver.Target(),
		SQL:        msg.sql,
		DurationMS: float64(msg.elapsed.Microseconds()) / 1000,
	}
	if msg.err != nil {
		entry.Error = msg.err.Error()
	} else {
		entry.Command, entry.Rows = msg.res.Command, len(msg.res.Rows)
	}
	_ = t.session.queryLog.Append(entry)
}

func (t *Tab) View() string {
	divider := ui.DimStyle.Render(strings.Repeat("─", t.width))
	view := ui.HighlightSQL(t.editor.View(), t.editorGutter()) + "\n" + t.promptView() + "\n" + divider + "\n" + t.resultsPane() + "\n" + t.status()
	if t.suggest != nil && t.focus == focusEditor {
		list := t.suggest.View()
		x, y := t.suggestOrigin(list, lipgloss.Height(view))
		view = ui.Overlay(view, list, x, y)
	}
	return view
}

func (t *Tab) resultsPane() string {
	body := ""
	switch {
	case t.panel != nil:
		body = t.panel.View()
	case t.err != nil:
		body = ui.ErrorStyle.Render(t.err.Error())
	case t.result != nil && len(t.result.Columns) > 0:
		body = t.results.View()
	}
	pane := lipgloss.NewStyle().Width(t.width).Height(t.resultsHeight).MaxHeight(t.resultsHeight).Render(body)
	if t.menu != nil {
		x, y := t.menuOrigin()
		pane = ui.Overlay(pane, t.menu.View(), x, y)
	}
	return pane
}

func (t *Tab) status() string {
	keys := map[focusArea]string{
		focusEditor:  "ctrl+r run · ctrl+k ask AI · ctrl+t new tab · F1 help",
		focusPrompt:  "enter ask · esc/ctrl+j editor",
		focusResults: "enter actions · y/Y copy · ctrl+j editor · ? help",
	}[t.focus]
	parts := []string{}
	switch {
	case t.asking:
		parts = append(parts, "✦ asking Claude (ctrl+c to cancel)")
	case t.running:
		parts = append(parts, "running… (ctrl+c to cancel)")
	case t.err != nil:
		parts = append(parts, ui.ErrorStyle.Render("error"))
	case t.result != nil:
		parts = append(parts, t.summary())
	}
	if t.notice != "" {
		parts = append(parts, t.notice)
	}
	return ui.KeyLabels(strings.Join(append(parts, ui.DimStyle.Render(keys)), " · "))
}

func (t *Tab) summary() string {
	r := t.result
	parts := []string{r.Command}
	if len(r.Columns) > 0 {
		rows := fmt.Sprintf("%d rows", len(r.Rows))
		if r.Truncated {
			rows = fmt.Sprintf("%d+ rows (limit)", len(r.Rows))
		}
		parts = []string{rows}
		if t.results.HasRows() {
			row, col := t.results.Cursor()
			parts = append(parts, fmt.Sprintf("row %d/%d · %s", row+1, len(r.Rows), r.Columns[col]))
		}
	}
	if r.Statements > 1 {
		parts = append(parts, fmt.Sprintf("%d statements", r.Statements))
	}
	parts = append(parts, fmt.Sprintf("%.1f ms", float64(r.Elapsed.Microseconds())/1000))
	return strings.Join(parts, " · ")
}
