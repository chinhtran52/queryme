package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/history"
	"github.com/chinhtran52/queryme/internal/secret"
)

func typeText(p *Picker, s string) {
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func enter(p *Picker) tea.Cmd { return p.Update(tea.KeyMsg{Type: tea.KeyEnter}) }

func TestPickerAsksForHiddenPasswordAfterURL(t *testing.T) {
	p := NewPicker(nil, nil, true)
	typeText(p, "postgres://app@db/app")
	enter(p)
	enter(p) // no SSH tunnel
	enter(p) // no name
	typeText(p, "s3cret")

	if view := p.View(); !strings.Contains(view, "Password") || strings.Contains(view, "s3cret") || !strings.Contains(view, "••••••") {
		t.Fatalf("password not hidden:\n%s", view)
	}

	msg := enter(p)().(PickedMsg)
	if msg.Connection.URL != "postgres://app@db/app" || msg.Connection.Password != "s3cret" {
		t.Fatalf("picked %+v", msg.Connection)
	}

	p.SetError(errors.New("auth failed"))
	if p.step != stepURL || p.password.Value() != "" {
		t.Fatalf("after failure: step=%d password=%q", p.step, p.password.Value())
	}
}

func TestPickerSaysWhetherPasswordIsSaved(t *testing.T) {
	for saves, want := range map[bool]string{true: "saved encrypted", false: "not saved: no OS keyring"} {
		p := NewPicker(nil, nil, saves)
		typeText(p, "postgres://app@db/app")
		enter(p)
		enter(p) // no SSH tunnel
		enter(p) // no name
		if view := p.View(); !strings.Contains(view, want) {
			t.Errorf("savesPasswords=%v: view missing %q:\n%s", saves, want, view)
		}
	}
}

func TestPickerEscFromPasswordReturnsToURL(t *testing.T) {
	p := NewPicker(nil, nil, true)
	typeText(p, "postgres://app@db/app")
	enter(p)
	enter(p) // no SSH tunnel
	enter(p) // no name
	typeText(p, "x")
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.step != stepURL || p.password.Value() != "" || p.url.Value() != "postgres://app@db/app" {
		t.Fatalf("step=%d url=%q password=%q", p.step, p.url.Value(), p.password.Value())
	}
}

func TestPickerReconnectsToLastConnection(t *testing.T) {
	recent := []history.Entry{
		{URL: "postgres://app@db/app", LastUsed: time.Now()},
		{URL: "postgres://old@db/old", LastUsed: time.Now().Add(-3 * time.Hour)},
	}
	p := NewPicker(recent, []config.Connection{{Name: "prod", URL: "x"}}, true)

	view := p.View()
	for _, want := range []string{"Recent", "▶ postgres://app@db/app", "reconnect", "3h ago", "Saved", "prod"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if got := enter(p)().(PickedMsg).Connection.URL; got != "postgres://app@db/app" {
		t.Fatalf("reconnected to %q", got)
	}
}

func TestPickerReconnectsWithSavedPasswordAndAsksAgainOnFailure(t *testing.T) {
	key := make([]byte, 32)
	cipher, _ := secret.NewCipher(key)
	store, _ := history.Open(filepath.Join(t.TempDir(), "connections.json"), cipher)
	if err := store.Record(config.Connection{URL: "postgres://app@db/app", Password: "saved", ReadOnly: true}, time.Now()); err != nil {
		t.Fatal(err)
	}

	p := NewPicker(store.Entries(), nil, true)
	if c := enter(p)().(PickedMsg).Connection; c.Password != "saved" || !c.ReadOnly {
		t.Fatalf("picked %+v", c)
	}

	p.SetError(errors.New("password authentication failed"))
	if p.step != stepPassword || p.url.Value() != "postgres://app@db/app" {
		t.Fatalf("after failure: step=%d url=%q", p.step, p.url.Value())
	}
	typeText(p, "new")
	if c := enter(p)().(PickedMsg).Connection; c.Password != "new" || !c.ReadOnly {
		t.Fatalf("retry picked %+v", c)
	}
}

func TestPickerAsksPasswordForRememberedReadOnlyConnection(t *testing.T) {
	p := NewPicker([]history.Entry{{URL: "postgres://app@db/app", ReadOnly: true, NeedsPassword: true}}, nil, true)
	enter(p)
	if p.step != stepPassword || p.url.Value() != "postgres://app@db/app" {
		t.Fatalf("step=%d url=%q", p.step, p.url.Value())
	}
	typeText(p, "pw")
	c := enter(p)().(PickedMsg).Connection
	if !c.ReadOnly || c.Password != "pw" {
		t.Fatalf("picked %+v", c)
	}

	p.SetError(errors.New("auth failed"))
	enter(p)
	enter(p) // no SSH tunnel
	enter(p) // no name
	typeText(p, "pw2")
	if c := enter(p)().(PickedMsg).Connection; !c.ReadOnly {
		t.Fatalf("retry lost read-only: %+v", c)
	}
}

func TestPickerForgetsRecentAfterConfirm(t *testing.T) {
	recent := []history.Entry{{URL: "postgres://a@db/one"}, {URL: "postgres://a@db/two"}}
	p := NewPicker(recent, []config.Connection{{Name: "prod", URL: "x"}}, true)
	p.Update(tea.KeyMsg{Type: tea.KeyDown})

	typeText(p, "d")
	if view := p.View(); !strings.Contains(view, "Forget postgres://a@db/two") {
		t.Fatalf("no confirmation:\n%s", view)
	}
	if cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}); cmd != nil || p.step != stepList {
		t.Fatalf("n should cancel: step=%d", p.step)
	}

	typeText(p, "d")
	msg := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})().(ForgetMsg)
	if msg.URL != "postgres://a@db/two" {
		t.Fatalf("forgot %q", msg.URL)
	}

	p.SetRecent(recent[:1])
	if p.step != stepList || p.cursor != 1 || !strings.Contains(p.View(), "▶ prod") {
		t.Fatalf("after forget: step=%d cursor=%d\n%s", p.step, p.cursor, p.View())
	}
}

func TestPickerForgetIgnoresSavedAndOpensURLWhenEmpty(t *testing.T) {
	p := NewPicker([]history.Entry{{URL: "postgres://a@db/one"}}, nil, true)
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	typeText(p, "d")
	if p.step != stepList {
		t.Fatalf("d on New connection changed step to %d", p.step)
	}

	p.SetRecent(nil)
	if p.step != stepURL {
		t.Fatalf("empty picker should ask for a URL, step=%d", p.step)
	}
}

func TestPickerNeverShowsRestoredSSLPassword(t *testing.T) {
	cipher, _ := secret.NewCipher(make([]byte, 32))
	store, _ := history.Open(filepath.Join(t.TempDir(), "connections.json"), cipher)
	if err := store.Record(config.Connection{URL: "postgres://app@db/app?sslpassword=K3Y", Password: "pw"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	p := NewPicker(store.Entries(), nil, true)
	enter(p)
	p.SetError(errors.New("auth failed"))
	if view := p.View(); strings.Contains(view, "K3Y") {
		t.Fatalf("view shows the sslpassword:\n%s", view)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	enter(p)
	enter(p) // no SSH tunnel
	enter(p) // no name
	typeText(p, "pw2")
	if c := enter(p)().(PickedMsg).Connection; !strings.Contains(c.URL, "sslpassword=K3Y") {
		t.Fatalf("retry lost the sslpassword: %q", c.URL)
	}
}

func TestPickerAsksForOptionalSSHTunnel(t *testing.T) {
	p := NewPicker(nil, nil, true)
	typeText(p, "postgres://app@db/app")
	enter(p)
	if view := p.View(); p.step != stepSSH || !strings.Contains(view, "SSH tunnel") {
		t.Fatalf("step=%d\n%s", p.step, view)
	}
	typeText(p, "me@bastion")
	enter(p)
	enter(p) // no name
	if view := p.View(); !strings.Contains(view, "me@bastion") || !strings.Contains(view, "Password") {
		t.Fatalf("password step should show the tunnel:\n%s", view)
	}
	c := enter(p)().(PickedMsg).Connection
	if c.SSH != "me@bastion" || c.URL != "postgres://app@db/app" {
		t.Fatalf("picked %+v", c)
	}

	p.SetError(errors.New("ssh: handshake failed"))
	enter(p)
	if p.ssh.Value() != "me@bastion" {
		t.Fatalf("retry lost the tunnel: %q", p.ssh.Value())
	}
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.step != stepURL {
		t.Fatalf("esc from SSH step: step=%d", p.step)
	}
}

func TestPickerReconnectsAndForgetsThroughTunnel(t *testing.T) {
	recent := []history.Entry{{URL: "postgres://app@db/app", SSH: "me@bastion", LastUsed: time.Now()}}
	p := NewPicker(recent, nil, true)
	if view := p.View(); !strings.Contains(view, "via me@bastion") {
		t.Fatalf("view:\n%s", view)
	}
	if c := enter(p)().(PickedMsg).Connection; c.SSH != "me@bastion" {
		t.Fatalf("picked %+v", c)
	}
	typeText(p, "d")
	if msg := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})().(ForgetMsg); msg.SSH != "me@bastion" {
		t.Fatalf("forget %+v", msg)
	}
}

func TestPickerNamesNewConnection(t *testing.T) {
	p := NewPicker(nil, nil, true)
	typeText(p, "postgres://app@db/app")
	enter(p)
	enter(p) // no SSH tunnel
	if view := p.View(); p.step != stepName || !strings.Contains(view, "Name") || !strings.Contains(view, "optional") {
		t.Fatalf("step=%d\n%s", p.step, view)
	}
	typeText(p, "  prod replica ")
	enter(p)
	if c := enter(p)().(PickedMsg).Connection; c.Name != "prod replica" {
		t.Fatalf("picked %+v", c)
	}
}

func TestPickerShowsAndRenamesRecent(t *testing.T) {
	recent := []history.Entry{{URL: "postgres://app@db/app", Name: "prod", LastUsed: time.Now()}, {URL: "postgres://b@db/b"}}
	p := NewPicker(recent, nil, true)
	if view := p.View(); !strings.Contains(view, "▶ prod") || !strings.Contains(view, "postgres://app@db/app") {
		t.Fatalf("view:\n%s", view)
	}

	typeText(p, "r")
	if p.step != stepRename || p.name.Value() != "prod" {
		t.Fatalf("step=%d name=%q", p.step, p.name.Value())
	}
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.step != stepList {
		t.Fatalf("esc should cancel, step=%d", p.step)
	}

	typeText(p, "r")
	typeText(p, "-eu")
	msg := enter(p)().(RenameMsg)
	if msg.URL != "postgres://app@db/app" || msg.Name != "prod-eu" || p.step != stepList {
		t.Fatalf("msg=%+v step=%d", msg, p.step)
	}
}
