package ui

import (
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"Editor", [][2]string{
		{"ctrl+r / F5", "run the editor SQL"},
		{"ctrl+c", "cancel a running query or AI request; quit when idle"},
		{"tab", "accept a suggestion when the list is open, else next pane"},
		{"↑↓ / ctrl+n ctrl+p", "pick a suggestion · esc hides the list"},
		{"ctrl+d / ctrl+x", "duplicate / cut the current line"},
		{"ctrl+v", "paste (falls back to queryme's last copy without a clipboard tool)"},
	}},
	{"AI", [][2]string{
		{"ctrl+k", "ask AI to write, fix or explain SQL"},
		{"enter / esc", "send the request / back to the editor"},
		{"/refresh", "reload the schema after DDL changes"},
	}},
	{"Results", [][2]string{
		{"↑↓←→ hjkl", "move the cell cursor · g/G first/last row · 0/$ first/last column"},
		{"enter / right click", "row actions: copy TSV, JSON or cell, view full row"},
		{"y / Y", "copy cell / copy row as TSV"},
		{"double click", "copy that cell's value"},
		{"esc", "back to the editor"},
	}},
	{"Tabs and panes", [][2]string{
		{"tab / shift+tab", "cycle editor → AI prompt → results"},
		{"ctrl+j", "jump to this tab's SQL editor"},
		{"ctrl+t / ctrl+w", "new tab / close tab"},
		{"ctrl+shift+1…9, ctrl+pgup/pgdn", "switch tab (or click it) · alt+1…9 where the terminal sends it"},
		{"F1 or ? in results", "this help · ctrl+q quit"},
	}},
}

// keyLabels spells modifiers the way the platform does: ⌃ ⌥ ⇧ on macOS, ctrl+ alt+ shift+ elsewhere.
var keyLabels = strings.NewReplacer()

func init() {
	if runtime.GOOS == "darwin" {
		keyLabels = strings.NewReplacer("ctrl+", "⌃", "alt+", "⌥", "shift+", "⇧")
	}
}

// KeyLabels rewrites the key names in a hint for the current platform.
func KeyLabels(s string) string { return keyLabels.Replace(s) }

// HelpView is the key reference drawn over the screen.
func HelpView() string {
	keyWidth := 0
	for _, section := range helpSections {
		for _, k := range section.keys {
			keyWidth = max(keyWidth, lipgloss.Width(KeyLabels(k[0])))
		}
	}
	key := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Width(keyWidth + 2)
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("queryme keys") + DimStyle.Render("  · any key closes") + "\n")
	for _, section := range helpSections {
		b.WriteString("\n" + lipgloss.NewStyle().Bold(true).Underline(true).Render(section.title) + "\n")
		for _, k := range section.keys {
			b.WriteString(key.Render(KeyLabels(k[0])) + KeyLabels(k[1]) + "\n")
		}
	}
	return menuBox.Render(strings.TrimRight(b.String(), "\n"))
}
