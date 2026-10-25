package clip

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

// Clipboard copies through a system tool, or an OSC 52 escape sequence when none works or over SSH,
// where a system tool would fill the remote machine's clipboard.
type Clipboard struct {
	getenv   func(string) string
	terminal io.Writer
}

func New() *Clipboard {
	return &Clipboard{getenv: os.Getenv, terminal: os.Stdout}
}

func (c *Clipboard) local() bool {
	return c.getenv("SSH_TTY") == "" && c.getenv("SSH_CONNECTION") == ""
}

// Copy returns how the text was copied.
func (c *Clipboard) Copy(text string) (string, error) {
	if c.local() {
		for _, cmd := range c.systemCommands() {
			if run(cmd, text) == nil {
				return "clipboard", nil
			}
		}
	}
	seq := osc52.New(text)
	if c.getenv("TMUX") != "" {
		seq = seq.Tmux()
	}
	if _, err := seq.WriteTo(c.terminal); err != nil {
		return "", err
	}
	return "terminal (OSC 52)", nil
}

func (c *Clipboard) systemCommands() [][]string {
	if runtime.GOOS == "darwin" {
		return [][]string{{"pbcopy"}}
	}
	var cmds [][]string
	if c.getenv("WAYLAND_DISPLAY") != "" {
		cmds = append(cmds, []string{"wl-copy"})
	}
	if c.getenv("DISPLAY") != "" {
		cmds = append(cmds, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	}
	return cmds
}

// Paste reads the system clipboard. It fails over SSH, where the tools would read the remote machine's clipboard,
// and when no tool is installed; terminals offer no reliable way to read it back through OSC 52.
func (c *Clipboard) Paste() (string, error) {
	if !c.local() {
		return "", errors.New("the local clipboard is not reachable over SSH")
	}
	for _, args := range c.pasteCommands() {
		if _, err := exec.LookPath(args[0]); err != nil {
			continue
		}
		if out, err := exec.Command(args[0], args[1:]...).Output(); err == nil {
			return string(out), nil
		}
	}
	return "", errors.New("no clipboard tool found: install wl-clipboard or xclip")
}

func (c *Clipboard) pasteCommands() [][]string {
	if runtime.GOOS == "darwin" {
		return [][]string{{"pbpaste"}}
	}
	var cmds [][]string
	if c.getenv("WAYLAND_DISPLAY") != "" {
		cmds = append(cmds, []string{"wl-paste", "--no-newline"})
	}
	if c.getenv("DISPLAY") != "" {
		cmds = append(cmds, []string{"xclip", "-selection", "clipboard", "-o"}, []string{"xsel", "--clipboard", "--output"})
	}
	return cmds
}

func run(args []string, text string) error {
	if _, err := exec.LookPath(args[0]); err != nil {
		return errors.New(args[0] + " not installed")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
