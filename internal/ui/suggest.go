package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/chinhtran52/queryme/internal/complete"
)

const maxVisibleSuggestions = 8

var (
	suggestItem     = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "252", Dark: "236"})
	suggestSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12"))
)

// SuggestList is the autocomplete popup under the editor cursor.
type SuggestList struct {
	items    []complete.Suggestion
	selected int
}

func NewSuggestList(items []complete.Suggestion) *SuggestList {
	return &SuggestList{items: items}
}

func (l *SuggestList) Selected() complete.Suggestion { return l.items[l.selected] }

// Move changes the selection by step, wrapping around the ends.
func (l *SuggestList) Move(step int) {
	l.selected = (l.selected + step + len(l.items)) % len(l.items)
}

func (l *SuggestList) View() string {
	start := clamp(l.selected-maxVisibleSuggestions+1, 0, max(len(l.items)-maxVisibleSuggestions, 0))
	end := min(start+maxVisibleSuggestions, len(l.items))

	nameWidth, tagWidth := 0, 0
	for _, s := range l.items[start:end] {
		nameWidth = max(nameWidth, runewidth.StringWidth(s.Text))
		tagWidth = max(tagWidth, runewidth.StringWidth(tag(s)))
	}
	lines := make([]string, 0, end-start+1)
	for i := start; i < end; i++ {
		s := l.items[i]
		text := " " + fit(s.Text, nameWidth) + "  " + fit(tag(s), tagWidth) + " "
		if i == l.selected {
			lines = append(lines, suggestSelected.Render(text))
		} else {
			lines = append(lines, suggestItem.Render(" "+fit(s.Text, nameWidth)+"  ")+suggestItem.Faint(true).Render(fit(tag(s), tagWidth)+" "))
		}
	}
	if len(l.items) > maxVisibleSuggestions {
		footer := fmt.Sprintf(" %d/%d · tab accept · esc ", l.selected+1, len(l.items))
		lines = append(lines, suggestItem.Faint(true).Render(fit(footer, nameWidth+tagWidth+4)))
	}
	return strings.Join(lines, "\n")
}

// tag is the grey label after a name: the kind, plus where a column or table comes from.
func tag(s complete.Suggestion) string {
	if s.Detail == "" {
		return s.Kind.String()
	}
	return s.Kind.String() + " · " + s.Detail
}
