package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func withColors(t *testing.T) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
}

// classesOf returns the visible text of each coloured run in a highlighted line, keyed by its escape code.
func coloured(line string, class tokenClass) []string {
	on := sgr(tokenStyles[class])
	var out []string
	for _, part := range strings.Split(line, on)[1:] {
		out = append(out, ansi.Strip(strings.SplitN(part, resetStyle, 2)[0]))
	}
	return out
}

func TestHighlightSQLColoursTokensAndKeepsText(t *testing.T) {
	withColors(t)
	line := "┃   1 select id, 'it''s' AS name, 42 FROM users -- the end"
	out := HighlightSQL(line, 6)

	if ansi.Strip(out) != line {
		t.Fatalf("visible text changed:\n%q\n%q", ansi.Strip(out), line)
	}
	checks := map[tokenClass][]string{
		keywordToken: {"select", "AS", "FROM"},
		stringToken:  {"'it''s'"},
		numberToken:  {"42"},
		commentToken: {"-- the end"},
	}
	for class, want := range checks {
		if got := coloured(out, class); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("class %d: got %q want %q", class, got, want)
		}
	}
}

func TestHighlightSQLSkipsTheGutterAndSurvivesTheCursor(t *testing.T) {
	withColors(t)
	// The cursor splits SELECT into "SEL", a reversed "E", and "CT"; the gutter number 12 must stay plain.
	line := "┃  12 SEL\x1b[7mE\x1b[0mCT 7"
	out := HighlightSQL(line, 6)
	if ansi.Strip(out) != ansi.Strip(line) {
		t.Fatalf("visible text changed: %q", ansi.Strip(out))
	}
	if got := coloured(out, numberToken); strings.Join(got, "|") != "7" {
		t.Fatalf("numbers coloured: %q (the gutter must be skipped)", got)
	}
	if got := coloured(out, keywordToken); strings.Join(got, "") != "SELECT" {
		t.Fatalf("keyword pieces: %q", got)
	}
	if !strings.Contains(out, "\x1b[7m") {
		t.Fatal("the cursor's own styling must stay")
	}
}

func TestHighlightSQLLeavesPlainTextAndNoColorAlone(t *testing.T) {
	withColors(t)
	if line := "┃   1 users u"; HighlightSQL(line, 6) != line {
		t.Fatal("text with nothing to colour must be returned unchanged")
	}
	lipgloss.SetColorProfile(termenv.Ascii)
	if line := "┃   1 SELECT 1"; HighlightSQL(line, 6) != line {
		t.Fatal("no escape codes may be added when colours are off")
	}
}
