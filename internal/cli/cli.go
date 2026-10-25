package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/chinhtran52/queryme/internal/app"
	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/history"
	"github.com/chinhtran52/queryme/internal/secret"
	"github.com/chinhtran52/queryme/internal/update"
)

// version is stamped at build time with -ldflags "-X github.com/chinhtran52/queryme/internal/cli.version=…".
var version = "dev"

func Version() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

// CLI owns the command tree and its I/O, so commands can be run against buffers in tests.
type CLI struct {
	in         io.Reader
	out        io.Writer
	errOut     io.Writer
	getenv     func(string) string
	configPath string
	connection string
	ssh        string
	name       string
	latest     func(context.Context) (string, error)
	install    func(ctx context.Context, version, exe string) error
}

func New(in io.Reader, out, errOut io.Writer) *CLI {
	return &CLI{in: in, out: out, errOut: errOut, getenv: os.Getenv, latest: update.Latest, install: update.Install}
}

const updateCheckTimeout = 3 * time.Second

// Main runs the process and returns its exit code.
func Main() int {
	if err := New(os.Stdin, os.Stdout, os.Stderr).Execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "queryme:", err)
		return 1
	}
	return 0
}

func (c *CLI) Execute(args []string) error {
	notice := c.checkForUpdate(args)
	root := c.rootCommand()
	root.SetArgs(args)
	err := root.Execute()
	// Never wait on the network: a check that hasn't answered by now is skipped this run.
	select {
	case latest := <-notice:
		fmt.Fprintf(c.errOut, "queryme %s is available (you have %s). Run `queryme update` to upgrade.\n", latest, Version())
	default:
	}
	return err
}

// checkForUpdate looks up the latest release in the background and sends it only when it is newer.
// QUERYME_NO_UPDATE_CHECK=1 turns it off; dev builds, completion and update itself skip it.
func (c *CLI) checkForUpdate(args []string) <-chan string {
	notice := make(chan string, 1)
	if c.getenv("QUERYME_NO_UPDATE_CHECK") != "" || !update.Released(Version()) ||
		len(args) > 0 && (args[0] == "update" || args[0] == "completion" || strings.HasPrefix(args[0], "__complete")) {
		return notice
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		if latest, err := c.latest(ctx); err == nil && update.Newer(latest, Version()) {
			notice <- latest
		}
	}()
	return notice
}

func (c *CLI) updateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update queryme to the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			latest, err := c.latest(ctx)
			if err != nil {
				return fmt.Errorf("checking for updates: %w", err)
			}
			current := Version()
			if update.Released(current) && !update.Newer(latest, current) {
				fmt.Fprintf(c.out, "queryme %s is up to date\n", current)
				return nil
			}
			exe, err := os.Executable()
			if err == nil {
				exe, err = filepath.EvalSymlinks(exe)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(c.out, "Updating queryme %s → %s...\n", current, latest)
			if err := c.install(ctx, latest, exe); err != nil {
				return fmt.Errorf("update failed: %w (rerun with write access to %s, or use install.sh)", err, filepath.Dir(exe))
			}
			fmt.Fprintf(c.out, "Updated %s to %s\n", exe, latest)
			return nil
		},
	}
}

func (c *CLI) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "queryme [postgres-url]",
		Short: "An AI-first PostgreSQL client for the terminal",
		Long: `queryme opens an interactive SQL client in the terminal.

With no arguments it shows a picker of recent connections, saved profiles and
the DATABASE_URL / PG* environment connection.`,
		Example: `  queryme
  queryme postgres://app@localhost:5432/app
  queryme --name local postgres://app@localhost:5432/app
  queryme -c prod
  queryme --ssh me@bastion postgres://app@db.internal/app
  queryme query -c prod "SELECT now()"`,
		Args:          cobra.MaximumNArgs(1),
		Version:       Version(),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          c.runTUI,
	}
	root.SetIn(c.in)
	root.SetOut(c.out)
	root.SetErr(c.errOut)
	root.PersistentFlags().StringVar(&c.configPath, "config", "", "config file (default ~/.config/queryme/config.toml)")
	root.PersistentFlags().StringVarP(&c.connection, "connection", "c", "", "saved connection profile to use")
	root.PersistentFlags().StringVar(&c.ssh, "ssh", "", "tunnel through this SSH server, [user@]host[:port]; overrides the profile's ssh")
	root.Flags().StringVar(&c.name, "name", "", "name to remember a postgres-url argument by in the picker")
	_ = root.RegisterFlagCompletionFunc("connection", c.completeProfiles)
	root.AddCommand(c.queryCommand(), c.connectionsCommand(), c.versionCommand(), c.updateCommand())
	return root
}

func (c *CLI) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the queryme version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "queryme", Version())
		},
	}
}

func (c *CLI) loadConfig() (config.Config, string, error) {
	path := c.configPath
	if path == "" {
		var err error
		if path, err = config.DefaultPath(); err != nil {
			return config.Config{}, "", err
		}
	}
	cfg, err := config.Load(path)
	return cfg, path, err
}

func (c *CLI) completeProfiles(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	cfg, _, err := c.loadConfig()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, len(cfg.Connections))
	for i, conn := range cfg.Connections {
		names[i] = conn.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func (c *CLI) profile(cfg config.Config, path string) (config.Connection, error) {
	conn, ok := cfg.Find(c.connection)
	if !ok {
		return conn, fmt.Errorf("no connection named %q in %s", c.connection, path)
	}
	return conn, nil
}

func (c *CLI) runTUI(_ *cobra.Command, args []string) error {
	cfg, path, err := c.loadConfig()
	if err != nil {
		return err
	}
	if c.name != "" && len(args) == 0 {
		return errors.New("--name needs a connection URL; in the picker, type the name after the URL")
	}
	var start *config.Connection
	switch {
	case len(args) == 1:
		start = &config.Connection{Name: c.name, URL: args[0]}
	case c.connection != "":
		conn, err := c.profile(cfg, path)
		if err != nil {
			return err
		}
		start = &conn
	}
	if c.ssh != "" {
		if start == nil {
			return errors.New("--ssh needs a connection URL or -c <profile>; in the picker, type the SSH host after the URL")
		}
		start.SSH = c.ssh
	}

	hist, err := c.openHistory(true)
	if err != nil {
		return err
	}
	dataDir, err := history.DefaultDir()
	if err != nil {
		return err
	}
	a := app.New(cfg, hist, history.NewQueryLog(filepath.Join(dataDir, "queries.jsonl")), start)
	defer a.Close()
	_, err = tea.NewProgram(a, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithInput(c.in), tea.WithOutput(c.out)).Run()
	return err
}

// openHistory degrades instead of failing: without a key passwords are not saved, and an unreadable file starts empty.
// withPasswords false skips the keyring for commands that never need a password.
func (c *CLI) openHistory(withPasswords bool) (*history.Store, error) {
	dir, err := history.DefaultDir()
	if err != nil {
		return nil, err
	}
	var cipher *secret.Cipher
	if withPasswords {
		key, err := secret.LoadKey()
		if err == nil {
			cipher, err = secret.NewCipher(key)
		}
		if err != nil {
			fmt.Fprintf(c.errOut, "queryme: saved passwords unavailable: %v\n", err)
		}
	}
	path := filepath.Join(dir, "connections.json")
	hist, err := history.Open(path, cipher)
	if err != nil {
		fmt.Fprintf(c.errOut, "queryme: ignoring unreadable %s: %v\n", path, err)
	}
	return hist, nil
}

var errNoConnection = errors.New("no connection: pass --url, -c <profile>, or set DATABASE_URL / PG* variables")
