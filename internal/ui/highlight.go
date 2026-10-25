package ui

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/chinhtran52/queryme/internal/complete"
)

type tokenClass int

const (
	plainToken tokenClass = iota
	keywordToken
	stringToken
	numberToken
	commentToken
)

var sqlKeywords = toSet(complete.Keywords)

var sqlToken = regexp.MustCompile(`--.*|/\*.*?(\*/|$)|'(?:[^']|'')*'?|\b\d+(?:\.\d+)?\b|[A-Za-z_][A-Za-z0-9_]*`)

var tokenStyles = map[tokenClass]lipgloss.Style{
	keywordToken: lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true),
	stringToken:  lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	numberToken:  lipgloss.NewStyle().Foreground(lipgloss.Color("13")),
	commentToken: lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true),
}

func toSet(words []string) map[string]bool {
	set := map[string]bool{}
	for _, w := range words {
		set[w] = true
	}
	return set
}

// HighlightSQL colours SQL in an already rendered editor view, leaving its first gutter cells of each line and
// its own styling (cursor, cursor line) alone. It adds escape codes only, so widths do not change.
// shortcut: lines are highlighted one at a time, so a string or /* comment */ spanning lines is only coloured on its
// first line; track state across lines if that shows up in practice.
func HighlightSQL(view string, gutter int) string {
	if sgr(tokenStyles[keywordToken]) == "" {
		return view // colours are off
	}
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		lines[i] = highlightLine(line, gutter)
	}
	return strings.Join(lines, "\n")
}

type segment struct {
	escape string // an escape sequence, or "" for text
	text   []rune
}

func highlightLine(line string, gutter int) string {
	segments := splitEscapes(line)

	// Gather the code after the gutter across segments, since the cursor splits words into separate runs.
	var code []rune
	col := 0
	for _, s := range segments {
		for _, r := range s.text {
			if col >= gutter {
				code = append(code, r)
			}
			col += runewidth.RuneWidth(r)
		}
	}
	classes := classify(string(code))
	if classes == nil {
		return line
	}

	var b strings.Builder
	state := ""
	col, pos := 0, 0
	for _, s := range segments {
		if s.escape != "" {
			b.WriteString(s.escape)
			if s.escape == resetStyle || s.escape == "\x1b[m" {
				state = ""
			} else {
				state += s.escape
			}
			continue
		}
		for start := 0; start < len(s.text); {
			r := s.text[start]
			if col < gutter {
				b.WriteRune(r)
				col += runewidth.RuneWidth(r)
				start++
				continue
			}
			class, end := classes[pos], start
			for end < len(s.text) && col >= gutter && classes[pos+end-start] == class {
				col += runewidth.RuneWidth(s.text[end])
				end++
			}
			run := string(s.text[start:end])
			if class == plainToken {
				b.WriteString(run)
			} else {
				b.WriteString(sgr(tokenStyles[class]) + run + resetStyle + state)
			}
			pos += end - start
			start = end
		}
	}
	return b.String()
}

// classify returns a class per rune of code, or nil when nothing needs colour.
func classify(code string) []tokenClass {
	matches := sqlToken.FindAllStringIndex(code, -1)
	if len(matches) == 0 {
		return nil
	}
	runeAt := make([]int, len(code)+1)
	n := 0
	for i := range code {
		runeAt[i] = n
		n++
	}
	runeAt[len(code)] = n

	classes := make([]tokenClass, n)
	colored := false
	for _, m := range matches {
		token := code[m[0]:m[1]]
		class := plainToken
		switch {
		case strings.HasPrefix(token, "--") || strings.HasPrefix(token, "/*"):
			class = commentToken
		case token[0] == '\'':
			class = stringToken
		case token[0] >= '0' && token[0] <= '9':
			class = numberToken
		case sqlKeywords[strings.ToUpper(token)]:
			class = keywordToken
		}
		if class != plainToken {
			colored = true
			for i := runeAt[m[0]]; i < runeAt[m[1]]; i++ {
				classes[i] = class
			}
		}
	}
	if !colored {
		return nil
	}
	return classes
}

// sgr returns the escape code that turns style on, or "" when colours are off.
func sgr(style lipgloss.Style) string {
	marked := style.Render("x")
	return marked[:strings.Index(marked, "x")]
}

func splitEscapes(line string) []segment {
	var segments []segment
	var text []rune
	flush := func() {
		if len(text) > 0 {
			segments = append(segments, segment{text: text})
			text = nil
		}
	}
	for i := 0; i < len(line); {
		if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
			end := i + 2
			for end < len(line) && (line[end] < 0x40 || line[end] > 0x7e) {
				end++
			}
			flush()
			segments = append(segments, segment{escape: line[i:min(end+1, len(line))]})
			i = end + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		text = append(text, r)
		i += size
	}
	flush()
	return segments
}
