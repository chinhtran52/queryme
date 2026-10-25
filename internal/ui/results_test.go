package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/chinhtran52/queryme/internal/db"
)

func text(s string) *string { return &s }

func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestResultTableCapsWidthAndShowsNullAndNewlines(t *testing.T) {
	r := NewResultTable(5)
	r.SetSize(80, 10)
	r.SetResult(&db.Result{
		Columns: []string{"id", "email"},
		Rows:    [][]*string{{text("1"), text("someone@example.com")}, {text("2"), nil}, {text("3"), text("a\nb")}},
	})

	if r.widths[0] != 2 || r.widths[1] != 5 {
		t.Fatalf("widths = %v", r.widths)
	}
	view := r.View()
	for _, want := range []string{"some…", "NULL", "a⏎b"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestResultTableFitsItsHeightAndWidth(t *testing.T) {
	r := NewResultTable(40)
	r.SetSize(30, 6)
	r.SetResult(&db.Result{Columns: []string{"id"}, Rows: [][]*string{{text("1")}, {text("2")}}})
	if got := lipgloss.Height(r.View()); got != 6 {
		t.Fatalf("height = %d, want 6", got)
	}
	for i, line := range strings.Split(r.View(), "\n") {
		if w := ansi.StringWidth(line); w != 30 {
			t.Errorf("line %d width = %d, want 30: %q", i, w, line)
		}
	}
}

func TestResultTableDarkensEveryLineWhenFocused(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	lipgloss.SetHasDarkBackground(true)

	r := NewResultTable(40)
	r.SetSize(40, 6)
	r.SetResult(&db.Result{Columns: []string{"id", "n"}, Rows: [][]*string{{text("1"), nil}, {text("2"), text("x")}}})
	const darker = "48;5;233"
	if strings.Contains(r.View(), darker) {
		t.Fatal("blurred table should keep the terminal background")
	}
	r.Focus()
	for i, line := range strings.Split(r.View(), "\n") {
		if i != 2 && !strings.Contains(line, darker) { // line 2 is the selected row
			t.Errorf("line %d lacks the focused background: %q", i, line)
		}
	}
}

func wideResult(cols, rows int) *db.Result {
	res := &db.Result{}
	for c := range cols {
		res.Columns = append(res.Columns, fmt.Sprintf("column_%02d", c))
	}
	for r := range rows {
		row := make([]*string, cols)
		for c := range cols {
			row[c] = text(fmt.Sprintf("r%dc%d", r, c))
		}
		res.Rows = append(res.Rows, row)
	}
	return res
}

func TestResultTableScrollsHorizontallyToTheCursor(t *testing.T) {
	r := NewResultTable(40)
	r.SetSize(50, 8)
	r.SetResult(wideResult(10, 3))
	if strings.Contains(r.View(), "column_09") {
		t.Fatal("last column should start off screen")
	}
	r.Update(key("$"))
	if _, col := r.Cursor(); col != 9 || r.left == 0 || !strings.Contains(r.View(), "column_09") || !r.fullyVisible(9) {
		t.Fatalf("col=%d left=%d view:\n%s", col, r.left, r.View())
	}
	r.Update(key("0"))
	if r.left != 0 {
		t.Fatalf("left = %d after jumping back", r.left)
	}
}

func TestResultTableScrollsVerticallyToTheCursor(t *testing.T) {
	r := NewResultTable(40)
	r.SetSize(50, 5) // three body rows
	r.SetResult(wideResult(1, 20))
	r.Update(key("G"))
	if row, _ := r.Cursor(); row != 19 || r.top != 17 || !strings.Contains(r.View(), "r19c0") {
		t.Fatalf("row=%d top=%d", row, r.top)
	}
}

func TestResultTableHitTest(t *testing.T) {
	r := NewResultTable(40)
	r.SetSize(80, 10)
	r.SetResult(wideResult(3, 4))
	// Gutter is " 4 │" (4 cells), then " column_00 │" is 12 cells wide.
	cases := []struct {
		x, y        int
		row, col    int
		ok          bool
		description string
	}{
		{0, 0, 0, 0, false, "header"},
		{5, 2, 0, 0, true, "first column, first row"},
		{17, 3, 1, 1, true, "second column, second row"},
		{2, 5, 3, 0, true, "gutter keeps the current column"},
		{79, 2, 0, 0, false, "empty space right of the columns"},
		{5, 9, 0, 0, false, "below the last row"},
	}
	for _, c := range cases {
		row, col, ok := r.HitTest(c.x, c.y)
		if ok != c.ok || (ok && (row != c.row || col != c.col)) {
			t.Errorf("%s: HitTest(%d,%d) = %d,%d,%v", c.description, c.x, c.y, row, col, ok)
		}
	}
}

func TestResultTableSwitchesToNarrowerResult(t *testing.T) {
	r := NewResultTable(40)
	r.SetSize(80, 10)
	r.SetResult(wideResult(3, 2))
	r.Update(key("$"))
	r.SetResult(&db.Result{Columns: []string{"x"}, Rows: [][]*string{{text("9")}}})
	if _, col := r.Cursor(); col != 0 || !strings.Contains(r.View(), "9") {
		t.Fatalf("col=%d view:\n%s", col, r.View())
	}
}

func TestResultTableIgnoresKeysWithoutRows(t *testing.T) {
	r := NewResultTable(40)
	r.Update(key("G"))
	r.SetResult(&db.Result{Columns: []string{"x"}})
	r.SetSize(20, 5)
	r.Update(key("G"))
	if _, _, ok := r.HitTest(3, 2); ok {
		t.Fatal("empty result should not be clickable")
	}
}
