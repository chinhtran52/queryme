package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type RowAction int

const (
	ActionNone RowAction = iota
	ActionClose
	ActionCopyRowTSV
	ActionCopyRowJSON
	ActionCopyCell
	ActionViewRow
)

var rowMenuItems = []struct {
	label  string
	action RowAction
}{
	{"Copy row (TSV)", ActionCopyRowTSV},
	{"Copy row (JSON)", ActionCopyRowJSON},
	{"Copy cell", ActionCopyCell},
	{"View full row", ActionViewRow},
}

var menuBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).Padding(0, 1)

// RowMenu is the action popup for one result row.
type RowMenu struct {
	title  string
	cursor int
}

func NewRowMenu(row int) *RowMenu {
	return &RowMenu{title: fmt.Sprintf("Row %d", row+1)}
}

func (m *RowMenu) HandleKey(key tea.KeyMsg) RowAction {
	switch key.String() {
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
	case "down", "j":
		m.cursor = min(m.cursor+1, len(rowMenuItems)-1)
	case "enter":
		return rowMenuItems[m.cursor].action
	case "esc", "q":
		return ActionClose
	}
	return ActionNone
}

// HandleClick takes a position relative to the menu's top-left corner; a click outside closes it.
func (m *RowMenu) HandleClick(x, y int) RowAction {
	width, height := m.Size()
	item := y - 2 // border, title
	if x < 0 || x >= width || y < 0 || y >= height {
		return ActionClose
	}
	if item < 0 || item >= len(rowMenuItems) {
		return ActionNone
	}
	m.cursor = item
	return rowMenuItems[item].action
}

func (m *RowMenu) Size() (int, int) {
	view := m.View()
	return lipgloss.Width(view), lipgloss.Height(view)
}

func (m *RowMenu) View() string {
	lines := []string{lipgloss.NewStyle().Bold(true).Render(m.title)}
	for i, item := range rowMenuItems {
		if i == m.cursor {
			lines = append(lines, CursorStyle.Render("▶ "+item.label))
		} else {
			lines = append(lines, "  "+item.label)
		}
	}
	return menuBox.Render(strings.Join(lines, "\n"))
}
