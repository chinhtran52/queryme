package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinhtran52/queryme/internal/db"
)

func TestRowMenuKeys(t *testing.T) {
	m := NewRowMenu(1)
	if !strings.Contains(m.View(), "Row 2") {
		t.Fatalf("view:\n%s", m.View())
	}
	m.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}); got != ActionCopyRowJSON {
		t.Fatalf("enter on second item = %v", got)
	}
	if got := m.HandleKey(tea.KeyMsg{Type: tea.KeyEsc}); got != ActionClose {
		t.Fatalf("esc = %v", got)
	}
}

func TestRowMenuClicks(t *testing.T) {
	m := NewRowMenu(0)
	w, _ := m.Size()
	if got := m.HandleClick(3, 5); got != ActionViewRow { // border, title, then four items
		t.Fatalf("click on 4th item = %v", got)
	}
	if got := m.HandleClick(3, 1); got != ActionNone {
		t.Fatalf("click on title = %v", got)
	}
	if got := m.HandleClick(w+2, 2); got != ActionClose {
		t.Fatalf("click outside = %v", got)
	}
}

func TestOverlayKeepsBackgroundOnBothSides(t *testing.T) {
	bg := "abcdefgh\nABCDEFGH\n12345678"
	got := ansi.Strip(Overlay(bg, "xy\nzw", 3, 1))
	if got != "abcdefgh\nABCxyFGH\n123zw678" {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRowViewerShowsFullValuesAndPrettyJSON(t *testing.T) {
	long := strings.Repeat("x", 120)
	res := &db.Result{Columns: []string{"long", "doc", "gone"}, Rows: [][]*string{{text(long), text(`{"a":1,"b":[2]}`), nil}}}
	v := NewRowViewer(res, 0, 60, 40)
	view := ansi.Strip(v.View())
	if strings.Count(view, "x") != 120 {
		t.Errorf("long value cut: %d x's", strings.Count(view, "x"))
	}
	for _, want := range []string{"Row 1 of 1", "\"a\": 1", "NULL"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if v.Update(tea.KeyMsg{Type: tea.KeyEsc}) {
		t.Fatal("esc should close the viewer")
	}
}
