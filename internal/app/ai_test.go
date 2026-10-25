package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/ui"
)

type fakeClaude struct {
	t   *testing.T
	dir string
	bin string
}

// newFakeClaude installs a script that records its arguments and stdin, then prints reply.json (or sleeps on "sleep").
func newFakeClaude(t *testing.T) *fakeClaude {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
d=$(dirname "$0")
for a in "$@"; do printf '%s\n' "$a"; done > "$d/args.txt"
cat > "$d/stdin.txt"
if [ "$(cat "$d/reply.json")" = sleep ]; then sleep 30; fi
cat "$d/reply.json"
`
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &fakeClaude{t: t, dir: dir, bin: bin}
}

func (f *fakeClaude) reply(text, session string) {
	f.t.Helper()
	body := `{"type":"result","subtype":"success","is_error":false,"session_id":"` + session + `","result":` + quote(text) + `}`
	if text == "sleep" {
		body = "sleep"
	}
	if err := os.WriteFile(filepath.Join(f.dir, "reply.json"), []byte(body), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeClaude) read(name string) string {
	data, _ := os.ReadFile(filepath.Join(f.dir, name))
	return string(data)
}

func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

func aiApp(t *testing.T, claude *fakeClaude) *App {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := config.Default()
	cfg.AI.ClaudeBin = claude.bin
	a := New(cfg, emptyHistory(t), nil, nil)
	send(a, tea.WindowSizeMsg{Width: 100, Height: 30})
	send(a, connectedMsg{driver: blockingDriver{}})
	return a
}

// ask types a request into the AI prompt and delivers the answer, following an openTabMsg if one comes back.
func ask(a *App, request string) {
	press(a, "ctrl+k")
	send(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(request)})
	if cmd := send(a, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		if msg, ok := aiAnswer(cmd); ok {
			if next, ok := quickMsg(send(a, msg)); ok {
				if _, isOpenTab := next.(openTabMsg); isOpenTab {
					send(a, next)
				}
			}
		}
	}
}

// aiAnswer runs the batch that Enter returns (the request plus the animation timer) and returns the answer.
func aiAnswer(cmd tea.Cmd) (tea.Msg, bool) {
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		return nil, false
	}
	done := make(chan tea.Msg, len(batch))
	for _, c := range batch {
		go func() { done <- c() }()
	}
	timeout := time.After(10 * time.Second)
	for {
		select {
		case msg := <-done:
			if _, isAnswer := msg.(aiDoneMsg); isAnswer {
				return msg, true
			}
		case <-timeout:
			return nil, false
		}
	}
}

// quickMsg runs cmd and returns its message unless it is a cursor-blink timer, which takes ~500ms and repeats forever.
func quickMsg(cmd tea.Cmd) (tea.Msg, bool) {
	if cmd == nil {
		return nil, false
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg, msg != nil
	case <-time.After(200 * time.Millisecond):
		return nil, false
	}
}

const sqlReply = "```sql\nSELECT count(*) FROM public.users;\n```\nCounts every user."

func TestAIWritesSQLIntoEmptyEditorWithoutRunning(t *testing.T) {
	claude := newFakeClaude(t)
	claude.reply(sqlReply, "s-1")
	a := aiApp(t, claude)

	ask(a, "count users")
	tab := a.activeTab()
	if got := tab.editor.Value(); got != "SELECT count(*) FROM public.users;" {
		t.Fatalf("editor = %q", got)
	}
	if tab.result != nil || tab.running || len(a.tabs) != 1 {
		t.Fatal("AI SQL must not run or open a tab when the editor was empty")
	}
	if tab.aiSession != "s-1" || tab.focus != focusEditor || tab.prompt.Value() != "" {
		t.Fatalf("session=%q focus=%v prompt=%q", tab.aiSession, tab.focus, tab.prompt.Value())
	}
	view := ansi.Strip(a.View())
	if !strings.Contains(view, "Counts every user.") || !strings.Contains(view, ui.KeyLabels("review it, then ctrl+r")) {
		t.Fatalf("notes or notice missing:\n%s", view)
	}
	if args := claude.read("args.txt"); !strings.Contains(args, "public.users(") || strings.Contains(args, "--resume") {
		t.Fatalf("first request args:\n%s", args)
	}

	claude.reply(sqlReply, "s-2")
	tab.editor.SetValue("")
	ask(a, "only active ones")
	if args := claude.read("args.txt"); !strings.Contains(args, "--resume\ns-1\n") {
		t.Fatalf("follow-up should resume the tab's session:\n%s", args)
	}
}

func TestAIOpensNewTabWhenEditorHasSQL(t *testing.T) {
	claude := newFakeClaude(t)
	claude.reply(sqlReply, "s-9")
	a := aiApp(t, claude)
	typeSQL(a, "SELECT 1")

	ask(a, "count users")
	if len(a.tabs) != 2 || a.active != 1 {
		t.Fatalf("tabs=%d active=%d", len(a.tabs), a.active)
	}
	if a.tabs[0].editor.Value() != "SELECT 1" {
		t.Fatal("the original editor must not change")
	}
	second := a.tabs[1]
	if second.editor.Value() != "SELECT count(*) FROM public.users;" || second.aiSession != "s-9" || second.result != nil {
		t.Fatalf("new tab: sql=%q session=%q", second.editor.Value(), second.aiSession)
	}
	if !strings.Contains(ansi.Strip(a.tabBar()), "AI: count users") {
		t.Fatalf("tab bar: %q", ansi.Strip(a.tabBar()))
	}
}

func TestAIExplanationShowsNotesAndFixGetsTheError(t *testing.T) {
	claude := newFakeClaude(t)
	claude.reply("The join multiplies rows, so the count is too high.", "s-3")
	a := aiApp(t, claude)
	tab := a.activeTab()
	tab.err = errFake("column \"nme\" does not exist")
	typeSQL(a, "SELECT nme FROM users")

	ask(a, "explain the error")
	if tab.panel == nil || tab.focus != focusResults || !strings.Contains(ansi.Strip(a.View()), "The join multiplies rows") {
		t.Fatalf("notes not shown:\n%s", ansi.Strip(a.View()))
	}
	if stdin := claude.read("stdin.txt"); !strings.Contains(stdin, `column "nme" does not exist`) || !strings.Contains(stdin, "SELECT nme FROM users") {
		t.Fatalf("prompt lacks the error or the editor SQL:\n%s", stdin)
	}
	press(a, "esc")
	if tab.panel != nil {
		t.Fatal("esc should close the notes")
	}
}

func TestAIFailureAndRefreshAndCancel(t *testing.T) {
	claude := newFakeClaude(t)
	a := aiApp(t, claude)
	a.cfg.AI.ClaudeBin = filepath.Join(t.TempDir(), "missing-claude")
	send(a, connectedMsg{driver: blockingDriver{}}) // reconnect with the missing binary
	ask(a, "anything")
	if !strings.Contains(ansi.Strip(a.View()), "AI request failed") {
		t.Fatalf("missing claude should show an error:\n%s", ansi.Strip(a.View()))
	}

	if a.activeTab().prompt.Value() != "anything" {
		t.Fatal("a failed request should keep its text for a retry")
	}
	a.activeTab().prompt.Reset()
	ask(a, "/refresh")
	if !strings.Contains(a.activeTab().notice, "Reloading the schema") {
		t.Fatalf("notice = %q", a.activeTab().notice)
	}

	b := aiApp(t, claude)
	claude.reply("sleep", "")
	press(b, "ctrl+k")
	send(b, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("slow")})
	cmd := send(b, tea.KeyMsg{Type: tea.KeyEnter})
	done := make(chan tea.Msg, 1)
	go func() {
		msg, _ := aiAnswer(cmd)
		done <- msg
	}()
	if !strings.Contains(ansi.Strip(b.View()), "asking Claude") {
		t.Fatal("status should show the request")
	}
	press(b, "ctrl+c")
	select {
	case msg := <-done:
		send(b, msg)
	case <-time.After(5 * time.Second):
		t.Fatal("ctrl+c did not cancel the AI request")
	}
	if b.activeTab().notice != "AI request cancelled" || b.activeTab().Running() {
		t.Fatalf("notice=%q running=%v", b.activeTab().notice, b.activeTab().Running())
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }

func TestPromptBoxIsTallerAndDarkWhenFocused(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	lipgloss.SetHasDarkBackground(true)

	a := aiApp(t, newFakeClaude(t))
	tab := a.activeTab()
	const darker = "48;5;233"
	if strings.Contains(tab.promptView(), darker) {
		t.Fatal("the blurred prompt keeps the terminal background")
	}
	press(a, "ctrl+k")
	lines := strings.Split(tab.promptView(), "\n")
	if len(lines) != promptLines {
		t.Fatalf("prompt area is %d lines, want %d", len(lines), promptLines)
	}
	for _, empty := range []bool{true, false} {
		if !empty {
			send(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("top customers")})
		}
		for i, line := range strings.Split(tab.promptView(), "\n") {
			if ansi.StringWidth(line) != 100 {
				t.Errorf("empty=%v line %d: width %d", empty, i, ansi.StringWidth(line))
			}
			// Every visible run between two resets must carry the background, including the padding.
			for _, run := range strings.Split(line, "\x1b[0m") {
				if ansi.Strip(run) != "" && !strings.Contains(run, darker) {
					t.Errorf("empty=%v line %d: %q has no background", empty, i, ansi.Strip(run))
				}
			}
		}
	}
	if got := strings.Count(a.View(), "\n") + 1; got != 30 {
		t.Fatalf("screen is %d lines, want 30", got)
	}

	press(a, "esc")
	click(a.activeTab(), 3, tab.editorHeight+promptLines-1) // last line of the box
	if tab.focus != focusPrompt {
		t.Fatalf("clicking the box should focus it, focus=%v", tab.focus)
	}
}

func TestDotsAnimateWhileAsking(t *testing.T) {
	claude := newFakeClaude(t)
	claude.reply("sleep", "")
	a := aiApp(t, claude)
	tab := a.activeTab()
	press(a, "ctrl+k")
	send(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("slow one")})
	send(a, tea.KeyMsg{Type: tea.KeyEnter})

	var labels []string
	for range 4 {
		labels = append(labels, strings.SplitN(ansi.Strip(tab.promptView()), "  ", 2)[0])
		send(a, aiTickMsg{tab: tab.id})
	}
	want := []string{"✦ Asking Claude.", "✦ Asking Claude..", "✦ Asking Claude...", "✦ Asking Claude."}
	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Fatalf("labels = %q", labels)
	}

	lipgloss.SetColorProfile(termenv.ANSI256)
	green := tab.promptView()
	lipgloss.SetColorProfile(termenv.Ascii)
	if !strings.HasPrefix(green, "\x1b[1;92") { // bold bright green while running
		t.Fatalf("running label is not green: %q", strings.SplitN(green, "\n", 2)[0])
	}

	tab.Cancel()
	send(a, aiDoneMsg{tab: tab.id, err: context.Canceled})
	if cmd := send(a, aiTickMsg{tab: tab.id}); cmd != nil {
		t.Fatal("the animation must stop once the answer arrives")
	}
	if label := ansi.Strip(tab.promptView()); !strings.HasPrefix(label, "✦ Ask AI") {
		t.Fatalf("label after the answer: %q", label)
	}
}
