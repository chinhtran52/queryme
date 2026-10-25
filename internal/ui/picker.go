package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/history"
)

type PickedMsg struct{ Connection config.Connection }

// ForgetMsg asks the app to remove a recent connection from the history.
type ForgetMsg struct{ URL, SSH string }

// RenameMsg asks the app to set the display name of a recent connection.
type RenameMsg struct{ URL, SSH, Name string }

type pickerStep int

const (
	stepList pickerStep = iota
	stepURL
	stepSSH
	stepName
	stepPassword
	stepConfirmForget
	stepRename
)

// Picker lists recent connections first, so the cursor starts on "reconnect to last".
type Picker struct {
	recent   []history.Entry
	saved    []config.Connection
	cursor   int
	step     pickerStep
	url      textinput.Model
	ssh      textinput.Model
	name     textinput.Model
	password textinput.Model
	pending  config.Connection
	err      error

	savesPasswords bool
}

// savesPasswords is false without an OS keyring; the prompt then says the password is not kept.
func NewPicker(recent []history.Entry, saved []config.Connection, savesPasswords bool) *Picker {
	url := textinput.New()
	url.Placeholder = "postgres://user@host:5432/dbname?sslmode=require"
	ssh := textinput.New()
	ssh.Placeholder = "user@bastion:22"
	name := textinput.New()
	name.Placeholder = "e.g. prod replica"
	password := textinput.New()
	password.EchoMode = textinput.EchoPassword
	password.EchoCharacter = '•'
	p := &Picker{recent: recent, saved: saved, url: url, ssh: ssh, name: name, password: password, savesPasswords: savesPasswords}
	if p.newIndex() == 0 {
		p.enterURL()
	}
	return p
}

// SetError reports a failed connection and drops the typed password so it is not kept around.
// A failed pick from the list asks for the password, since a saved one may be stale.
func (p *Picker) SetError(err error) {
	p.err = err
	switch {
	case p.step == stepPassword:
		p.enterURL()
	case p.step == stepList && p.pending.URL != "":
		p.askPassword(p.pending)
	}
}

// SetRecent shows the history after it changed, keeping the cursor on the list.
func (p *Picker) SetRecent(recent []history.Entry) {
	p.recent = recent
	p.step = stepList
	p.cursor = min(p.cursor, p.newIndex())
	if p.newIndex() == 0 {
		p.enterURL()
	}
}

func (p *Picker) newIndex() int { return len(p.recent) + len(p.saved) }

func (p *Picker) enterURL() tea.Cmd {
	p.step = stepURL
	p.password.Reset()
	p.password.Blur()
	p.ssh.Blur()
	p.name.Blur()
	return p.url.Focus()
}

func (p *Picker) askName(c config.Connection) tea.Cmd {
	p.pending = c
	p.name.SetValue(c.Name)
	p.name.CursorEnd()
	p.ssh.Blur()
	p.step = stepName
	return p.name.Focus()
}

func (p *Picker) startRename() tea.Cmd {
	p.err = nil
	p.name.SetValue(p.recent[p.cursor].Name)
	p.name.CursorEnd()
	p.step = stepRename
	return p.name.Focus()
}

func (p *Picker) askSSH(c config.Connection) tea.Cmd {
	p.pending = c
	p.ssh.SetValue(c.SSH)
	p.ssh.CursorEnd()
	p.url.Blur()
	p.step = stepSSH
	return p.ssh.Focus()
}

func (p *Picker) askPassword(c config.Connection) tea.Cmd {
	p.pending = c
	p.pending.Password = ""
	// Redacted so a restored sslpassword never shows on screen.
	p.url.SetValue(history.Redact(c.URL))
	p.url.Blur()
	p.ssh.SetValue(c.SSH)
	p.ssh.Blur()
	p.name.SetValue(c.Name)
	p.name.Blur()
	p.step = stepPassword
	return p.password.Focus()
}

func (p *Picker) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch p.step {
	case stepURL:
		return p.updateURL(key)
	case stepSSH:
		return p.updateSSH(key)
	case stepName:
		return p.updateName(key)
	case stepRename:
		return p.updateRename(key)
	case stepPassword:
		return p.updatePassword(key)
	case stepConfirmForget:
		p.step = stepList
		if key.String() == "y" {
			e := p.recent[p.cursor]
			return func() tea.Msg { return ForgetMsg{URL: e.URL, SSH: e.SSH} }
		}
		return nil
	}
	switch key.String() {
	case "up", "k":
		p.cursor = max(p.cursor-1, 0)
	case "down", "j":
		p.cursor = min(p.cursor+1, p.newIndex())
	case "enter":
		return p.choose()
	case "d", "delete":
		if p.cursor < len(p.recent) {
			p.err = nil
			p.step = stepConfirmForget
		}
	case "r":
		if p.cursor < len(p.recent) {
			return p.startRename()
		}
	}
	return nil
}

func (p *Picker) choose() tea.Cmd {
	var c config.Connection
	switch i := p.cursor; {
	case i < len(p.recent):
		c = p.recent[i].Connection()
		if p.recent[i].MustAskPassword() {
			return p.askPassword(c)
		}
	case i < p.newIndex():
		c = p.saved[i-len(p.recent)]
	default:
		p.url.Reset()
		return p.enterURL()
	}
	p.pending = c
	p.pending.Password = ""
	return pick(c)
}

func (p *Picker) updateURL(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		if p.newIndex() > 0 {
			p.step = stepList
			p.url.Blur()
		}
		return nil
	case "enter":
		url := strings.TrimSpace(p.url.Value())
		if url == "" {
			return nil
		}
		// Retrying the same URL keeps its settings, so a read-only connection stays read-only.
		if url == history.Redact(p.pending.URL) {
			return p.askSSH(p.pending)
		}
		return p.askSSH(config.Connection{URL: url})
	}
	var cmd tea.Cmd
	p.url, cmd = p.url.Update(key)
	return cmd
}

func (p *Picker) updateSSH(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		return p.enterURL()
	case "enter":
		p.pending.SSH = strings.TrimSpace(p.ssh.Value())
		return p.askName(p.pending)
	}
	var cmd tea.Cmd
	p.ssh, cmd = p.ssh.Update(key)
	return cmd
}

func (p *Picker) updateName(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		return p.askSSH(p.pending)
	case "enter":
		p.pending.Name = strings.TrimSpace(p.name.Value())
		return p.askPassword(p.pending)
	}
	var cmd tea.Cmd
	p.name, cmd = p.name.Update(key)
	return cmd
}

func (p *Picker) updateRename(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		p.step = stepList
		p.name.Blur()
		return nil
	case "enter":
		p.step = stepList
		p.name.Blur()
		e, name := p.recent[p.cursor], strings.TrimSpace(p.name.Value())
		return func() tea.Msg { return RenameMsg{URL: e.URL, SSH: e.SSH, Name: name} }
	}
	var cmd tea.Cmd
	p.name, cmd = p.name.Update(key)
	return cmd
}

func (p *Picker) updatePassword(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		return p.enterURL()
	case "enter":
		c := p.pending
		c.Password = p.password.Value()
		return pick(c)
	}
	var cmd tea.Cmd
	p.password, cmd = p.password.Update(key)
	return cmd
}

func pick(c config.Connection) tea.Cmd {
	return func() tea.Msg { return PickedMsg{Connection: c} }
}

func (p *Picker) View() string {
	var b strings.Builder
	b.WriteString(HeaderStyle.Render("queryme") + "\n\n")
	switch p.step {
	case stepURL:
		b.WriteString("Connection URL:\n" + p.url.View() + "\n\n")
		b.WriteString(DimStyle.Render(KeyLabels("enter next · esc back · ctrl+q quit")))
	case stepSSH:
		b.WriteString("Connection URL:\n" + p.url.View() + "\n\n")
		b.WriteString("SSH tunnel " + DimStyle.Render("([user@]host[:port] · leave empty to connect directly)") + ":\n" + p.ssh.View() + "\n\n")
		b.WriteString(DimStyle.Render(KeyLabels("enter next · esc back · ctrl+q quit")))
	case stepName:
		b.WriteString("Connection URL:\n" + p.url.View() + "\n\n")
		if p.pending.SSH != "" {
			b.WriteString("SSH tunnel:\n" + p.ssh.View() + "\n\n")
		}
		b.WriteString("Name " + DimStyle.Render("(optional · shown in the Recent list instead of the URL)") + ":\n" + p.name.View() + "\n\n")
		b.WriteString(DimStyle.Render(KeyLabels("enter next · esc back · ctrl+q quit")))
	case stepPassword:
		b.WriteString("Connection URL:\n" + p.url.View() + "\n\n")
		if p.pending.SSH != "" {
			b.WriteString("SSH tunnel:\n" + p.ssh.View() + "\n\n")
		}
		if p.pending.Name != "" {
			b.WriteString("Name:\n" + p.name.View() + "\n\n")
		}
		saving := "saved encrypted"
		if !p.savesPasswords {
			saving = "not saved: no OS keyring"
		}
		b.WriteString("Password " + DimStyle.Render("("+saving+" · leave empty to use the URL or ~/.pgpass)") + ":\n" + p.password.View() + "\n\n")
		b.WriteString(DimStyle.Render(KeyLabels("enter connect · esc back · ctrl+q quit")))
	case stepConfirmForget:
		p.listView(&b)
		e := p.recent[p.cursor]
		b.WriteString("\n\n" + ErrorStyle.Render("Forget "+e.Label()+via(e.SSH)+" and its saved password? y/n"))
	case stepRename:
		p.listView(&b)
		b.WriteString("\n\nName for " + p.recent[p.cursor].URL + DimStyle.Render(" (empty shows the URL · enter save · esc cancel)") + ":\n" + p.name.View())
	default:
		p.listView(&b)
	}
	if p.err != nil {
		b.WriteString("\n\n" + ErrorStyle.Render(p.err.Error()))
	}
	return b.String()
}

func (p *Picker) listView(b *strings.Builder) {
	if len(p.recent) > 0 {
		b.WriteString(DimStyle.Render("Recent") + "\n")
		for i, e := range p.recent {
			note := ago(e.LastUsed)
			if i == 0 {
				note += " · reconnect"
			}
			label := e.URL
			if e.Name != "" {
				label = e.Name + DimStyle.Render("  "+e.URL)
			}
			b.WriteString(p.line(i, label+via(e.SSH), e.ReadOnly, note))
		}
		b.WriteString("\n")
	}
	if len(p.saved) > 0 {
		b.WriteString(DimStyle.Render("Saved") + "\n")
		for i, c := range p.saved {
			b.WriteString(p.line(len(p.recent)+i, c.Name+via(c.SSH), c.ReadOnly, ""))
		}
		b.WriteString("\n")
	}
	b.WriteString(p.line(p.newIndex(), "New connection…", false, ""))
	b.WriteString("\n" + DimStyle.Render(KeyLabels(p.listHint())))
}

func (p *Picker) listHint() string {
	if len(p.recent) > 0 {
		return "↑↓ move · enter connect · r rename · d forget recent · ctrl+q quit"
	}
	return "↑↓ move · enter connect · ctrl+q quit"
}

func (p *Picker) line(i int, name string, readOnly bool, note string) string {
	if readOnly {
		name += DimStyle.Render(" (read-only)")
	}
	if note != "" {
		name += DimStyle.Render("  " + note)
	}
	if i == p.cursor {
		return CursorStyle.Render("▶ ") + name + "\n"
	}
	return "  " + name + "\n"
}

func via(ssh string) string {
	if ssh == "" {
		return ""
	}
	return DimStyle.Render(" via " + ssh)
}

func ago(t time.Time) string {
	switch d := time.Since(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
