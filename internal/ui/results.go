package ui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/chinhtran52/queryme/internal/db"
)

const (
	nullText  = "NULL"
	headerRow = 2 // header line + separator line
	minCut    = 3 // narrowest partly visible column worth drawing
)

// FocusedBackground marks the focused pane: a step darker than common terminal backgrounds, in light and dark themes.
var FocusedBackground = lipgloss.AdaptiveColor{Light: "254", Dark: "233"}

var controlChars = strings.NewReplacer("\r\n", "⏎", "\n", "⏎", "\r", "⏎", "\t", "→")

// ResultTable is a grid with a cell cursor, horizontal scrolling by column and mouse hit-testing.
type ResultTable struct {
	result      *db.Result
	widths      []int
	maxColWidth int
	row, col    int
	top, left   int
	width       int
	height      int
	focused     bool
}

type visibleColumn struct{ index, width int }

func NewResultTable(maxColWidth int) *ResultTable {
	return &ResultTable{maxColWidth: maxColWidth}
}

func (r *ResultTable) SetResult(res *db.Result) {
	r.result = res
	r.row, r.col, r.top, r.left = 0, 0, 0, 0
	r.widths = make([]int, len(res.Columns))
	for i, name := range res.Columns {
		width := runewidth.StringWidth(name)
		for _, row := range res.Rows {
			width = max(width, runewidth.StringWidth(CellText(row[i])))
		}
		r.widths[i] = min(max(width, 1), r.maxColWidth)
	}
}

// CellText renders a value on one line: NULL as "NULL", newlines as ⏎ and tabs as →.
func CellText(cell *string) string {
	if cell == nil {
		return nullText
	}
	return controlChars.Replace(*cell)
}

func (r *ResultTable) SetSize(width, height int) {
	r.width, r.height = width, height
	r.scrollToCursor()
}

func (r *ResultTable) Focus()        { r.focused = true }
func (r *ResultTable) Blur()         { r.focused = false }
func (r *ResultTable) Focused() bool { return r.focused }

// Cursor returns the selected row and column.
func (r *ResultTable) Cursor() (int, int) { return r.row, r.col }

func (r *ResultTable) HasRows() bool { return r.result != nil && len(r.result.Rows) > 0 }

func (r *ResultTable) Select(row, col int) {
	if !r.HasRows() {
		return
	}
	r.row = clamp(row, 0, len(r.result.Rows)-1)
	r.col = clamp(col, 0, len(r.result.Columns)-1)
	r.scrollToCursor()
}

func (r *ResultTable) MoveBy(rows, cols int) { r.Select(r.row+rows, r.col+cols) }

func (r *ResultTable) Update(key tea.KeyMsg) {
	if !r.HasRows() {
		return
	}
	page := max(r.bodyHeight()-1, 1)
	switch key.String() {
	case "up", "k":
		r.MoveBy(-1, 0)
	case "down", "j":
		r.MoveBy(1, 0)
	case "left", "h":
		r.MoveBy(0, -1)
	case "right", "l":
		r.MoveBy(0, 1)
	case "pgup", "ctrl+u":
		r.MoveBy(-page, 0)
	case "pgdown", "ctrl+d":
		r.MoveBy(page, 0)
	case "home", "g":
		r.Select(0, r.col)
	case "end", "G":
		r.Select(len(r.result.Rows)-1, r.col)
	case "0":
		r.Select(r.row, 0)
	case "$":
		r.Select(r.row, len(r.result.Columns)-1)
	}
}

func (r *ResultTable) bodyHeight() int { return max(r.height-headerRow, 0) }

func (r *ResultTable) gutterWidth() int {
	if r.result == nil {
		return 0
	}
	return len(strconv.Itoa(len(r.result.Rows))) + 3 // space, digits, space, "│"
}

func (r *ResultTable) scrollToCursor() {
	if !r.HasRows() {
		return
	}
	if body := r.bodyHeight(); body > 0 {
		r.top = clamp(r.top, r.row-body+1, r.row)
	}
	r.left = min(r.left, r.col)
	for r.left < r.col && !r.fullyVisible(r.col) {
		r.left++
	}
}

func (r *ResultTable) fullyVisible(col int) bool {
	for _, c := range r.visibleColumns() {
		if c.index == col {
			return c.width == r.widths[col]
		}
	}
	return false
}

// visibleColumns lists the columns that fit from r.left on; the last one may be cut short.
func (r *ResultTable) visibleColumns() []visibleColumn {
	var cols []visibleColumn
	room := r.width - r.gutterWidth()
	for i := r.left; i < len(r.widths); i++ {
		need := r.widths[i] + 3 // one space each side, then "│"
		if need <= room {
			cols = append(cols, visibleColumn{i, r.widths[i]})
			room -= need
			continue
		}
		if room-3 >= minCut {
			cols = append(cols, visibleColumn{i, room - 3})
		}
		break
	}
	return cols
}

// HitTest maps a position inside the table to a cell. ok is false for the header and empty space.
func (r *ResultTable) HitTest(x, y int) (row, col int, ok bool) {
	if !r.HasRows() || y < headerRow {
		return 0, 0, false
	}
	row = r.top + y - headerRow
	if row >= len(r.result.Rows) || y-headerRow >= r.bodyHeight() {
		return 0, 0, false
	}
	pos := r.gutterWidth()
	if x < pos {
		return row, r.col, true
	}
	for _, c := range r.visibleColumns() {
		pos += c.width + 3
		if x < pos {
			return row, c.index, true
		}
	}
	return 0, 0, false
}

type tableStyles struct {
	base, dim, header, headerCursor, null, selected, selectedCell lipgloss.Style
}

func (r *ResultTable) styles() tableStyles {
	base := lipgloss.NewStyle()
	if r.focused {
		base = base.Background(FocusedBackground)
	}
	selected := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12"))
	return tableStyles{
		base:         base,
		dim:          base.Faint(true),
		header:       base.Bold(true),
		headerCursor: base.Bold(true).Underline(r.focused),
		null:         base.Faint(true).Italic(true),
		selected:     selected,
		selectedCell: selected.Bold(true).Underline(true),
	}
}

func (r *ResultTable) View() string {
	if r.result == nil || r.height < 1 {
		return ""
	}
	s := r.styles()
	cols := r.visibleColumns()
	lines := []string{r.headerLine(s, cols), s.dim.Render(strings.Repeat("─", r.width))}
	for i := r.top; i < len(r.result.Rows) && len(lines) < r.height; i++ {
		lines = append(lines, r.rowLine(s, cols, i))
	}
	for len(lines) < r.height {
		lines = append(lines, s.base.Render(strings.Repeat(" ", r.width)))
	}
	return strings.Join(lines[:r.height], "\n")
}

func (r *ResultTable) headerLine(s tableStyles, cols []visibleColumn) string {
	var b strings.Builder
	used := r.gutterWidth()
	b.WriteString(s.dim.Render(strings.Repeat(" ", used-1) + "│"))
	for _, c := range cols {
		style := s.header
		if c.index == r.col {
			style = s.headerCursor
		}
		b.WriteString(style.Render(" "+fit(r.result.Columns[c.index], c.width)+" ") + s.dim.Render("│"))
		used += c.width + 3
	}
	b.WriteString(s.base.Render(strings.Repeat(" ", max(r.width-used, 0))))
	return b.String()
}

func (r *ResultTable) rowLine(s tableStyles, cols []visibleColumn, row int) string {
	isCursor := row == r.row
	sep, fill := s.dim, s.base
	if isCursor {
		sep, fill = s.selected, s.selected
	}
	var b strings.Builder
	number := strconv.Itoa(row + 1)
	used := r.gutterWidth()
	b.WriteString(sep.Render(strings.Repeat(" ", used-2-len(number)) + number + " │"))
	for _, c := range cols {
		cell := r.result.Rows[row][c.index]
		style := s.base
		switch {
		case isCursor && c.index == r.col && r.focused:
			style = s.selectedCell
		case isCursor:
			style = s.selected
		case cell == nil:
			style = s.null
		}
		b.WriteString(style.Render(" "+fit(CellText(cell), c.width)+" ") + sep.Render("│"))
		used += c.width + 3
	}
	b.WriteString(fill.Render(strings.Repeat(" ", max(r.width-used, 0))))
	return b.String()
}

// fit cuts text to width with "…" and pads it to exactly width cells.
func fit(text string, width int) string {
	text = runewidth.Truncate(text, width, "…")
	return text + strings.Repeat(" ", max(width-runewidth.StringWidth(text), 0))
}

func clamp(v, low, high int) int { return min(max(v, low), high) }
