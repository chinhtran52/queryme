package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/db"
	"github.com/chinhtran52/queryme/internal/history"
)

func (c *CLI) connectionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "connections",
		Aliases: []string{"conn"},
		Short:   "Inspect known connections",
		Args:    cobra.NoArgs,
	}
	format := "table"
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the environment connection, saved profiles and recent connections",
		Long:    "List connections as the start screen shows them. Passwords are never printed.",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			write, ok := formats[format]
			if !ok {
				return fmt.Errorf("unknown format %q (want %s)", format, strings.Join(formatNames(), ", "))
			}
			res, err := c.connectionsResult()
			if err != nil {
				return err
			}
			return write(c.out, res)
		},
	}
	list.Flags().StringVarP(&format, "format", "f", "table", "output format: "+strings.Join(formatNames(), ", "))
	forget := &cobra.Command{
		Use:     "forget <url>",
		Aliases: []string{"rm"},
		Short:   "Remove a recent connection and its saved password",
		Long:    "Remove a recent connection as `connections list` shows it, with --ssh for one reached through a tunnel. A password in the URL is ignored when matching.",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			hist, err := c.openHistory(false)
			if err != nil {
				return err
			}
			url := history.Redact(args[0])
			removed, err := hist.Delete(url, c.ssh)
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("no recent connection %s%s", url, via(c.ssh))
			}
			fmt.Fprintf(c.out, "Forgot %s%s\n", url, via(c.ssh))
			return nil
		},
	}
	rename := &cobra.Command{
		Use:   "rename <url> [name]",
		Short: "Name a recent connection, or clear its name",
		Long:  "Set the name the picker shows for a recent connection, with --ssh for one reached through a tunnel. Without a name it shows the URL again.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			hist, err := c.openHistory(false)
			if err != nil {
				return err
			}
			url, name := history.Redact(args[0]), ""
			if len(args) == 2 {
				name = args[1]
			}
			found, err := hist.Rename(url, c.ssh, name)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("no recent connection %s%s", url, via(c.ssh))
			}
			fmt.Fprintf(c.out, "Renamed %s%s\n", url, via(c.ssh))
			return nil
		},
	}
	cmd.AddCommand(list, forget, rename)
	return cmd
}

// connectionsResult reuses the query output formats by shaping the list as a result set.
func (c *CLI) connectionsResult() (*db.Result, error) {
	cfg, _, err := c.loadConfig()
	if err != nil {
		return nil, err
	}
	hist, err := c.openHistory(false)
	if err != nil {
		return nil, err
	}
	res := &db.Result{Columns: []string{"source", "name", "url", "ssh", "read_only", "last_used"}}
	add := func(source, name, url, ssh string, readOnly bool, lastUsed string) {
		values := []string{source, name, history.Redact(url), ssh, yesNo(readOnly), lastUsed}
		row := make([]*string, len(values))
		for i := range values {
			row[i] = &values[i]
		}
		res.Rows = append(res.Rows, row)
	}

	if env, ok := config.EnvConnection(c.getenv); ok {
		add("env", env.Name, env.URL, "", false, "")
	}
	for _, conn := range cfg.Connections {
		add("saved", conn.Name, conn.URL, conn.SSH, conn.ReadOnly, "")
	}
	for _, e := range hist.Entries() {
		add("recent", e.Name, e.URL, e.SSH, e.ReadOnly, e.LastUsed.Local().Format("2006-01-02 15:04"))
	}
	return res, nil
}

func via(ssh string) string {
	if ssh == "" {
		return ""
	}
	return " via " + ssh
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
