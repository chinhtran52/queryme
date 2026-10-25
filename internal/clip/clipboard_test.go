package clip

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestCopyFallsBackToOSC52(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"no display": {},
		"over ssh":   {"SSH_TTY": "/dev/pts/1", "DISPLAY": ":0", "WAYLAND_DISPLAY": "wayland-0"},
	} {
		var out bytes.Buffer
		c := &Clipboard{getenv: func(k string) string { return env[k] }, terminal: &out}
		how, err := c.Copy("héllo")
		if err != nil || how != "terminal (OSC 52)" {
			t.Fatalf("%s: how=%q err=%v", name, how, err)
		}
		if want := base64.StdEncoding.EncodeToString([]byte("héllo")); !strings.Contains(out.String(), want) {
			t.Fatalf("%s: OSC 52 sequence %q lacks the payload", name, out.String())
		}
	}
}

func TestCopyWrapsOSC52ForTmux(t *testing.T) {
	var out bytes.Buffer
	env := map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0"}
	c := &Clipboard{getenv: func(k string) string { return env[k] }, terminal: &out}
	if _, err := c.Copy("x"); err != nil || !strings.HasPrefix(out.String(), "\x1bPtmux;") {
		t.Fatalf("err=%v seq=%q", err, out.String())
	}
}

func TestPasteFailsWithoutALocalTool(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"no display": {},
		"over ssh":   {"SSH_CONNECTION": "1.2.3.4 22 5.6.7.8 22", "DISPLAY": ":0"},
	} {
		c := &Clipboard{getenv: func(k string) string { return env[k] }}
		if _, err := c.Paste(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
