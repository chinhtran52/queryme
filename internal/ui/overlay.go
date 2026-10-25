package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const resetStyle = "\x1b[0m"

// KeepBackground re-applies style's background after every reset in s, so text drawn by a component
// that resets mid-line (such as a textarea placeholder) keeps it to the end of the line.
func KeepBackground(s string, style lipgloss.Style) string {
	on := sgr(style)
	if on == "" {
		return s // colours are off
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = on + strings.ReplaceAll(line, resetStyle, resetStyle+on) + resetStyle
	}
	return strings.Join(lines, "\n")
}

// Overlay draws fg over bg with its top-left corner at (x, y), keeping bg's styling on both sides.
func Overlay(bg, fg string, x, y int) string {
	lines := strings.Split(bg, "\n")
	for i, line := range strings.Split(fg, "\n") {
		row := y + i
		if row < 0 || row >= len(lines) {
			continue
		}
		under := lines[row]
		left := ansi.Truncate(under, x, "")
		left += strings.Repeat(" ", max(x-ansi.StringWidth(left), 0))
		right := ansi.TruncateLeft(under, x+ansi.StringWidth(line), "")
		lines[row] = left + resetStyle + line + resetStyle + right
	}
	return strings.Join(lines, "\n")
}
