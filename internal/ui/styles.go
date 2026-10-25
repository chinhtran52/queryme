package ui

import "github.com/charmbracelet/lipgloss"

var (
	HeaderStyle = lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1)
	DimStyle    = lipgloss.NewStyle().Faint(true)
	ErrorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	CursorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
)
