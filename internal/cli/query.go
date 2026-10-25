package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/db"
	"github.com/chinhtran52/queryme/internal/db/postgres"
)

const connectTimeout = 10 * time.Second

type queryOptions struct {
	url    string
	format string
	limit  int
}

func (c *CLI) queryCommand() *cobra.Command {
	opts := &queryOptions{}
	cmd := &cobra.Command{
		Use:   "query [sql]",
		Short: "Run SQL and print the result without opening the TUI",
		Long: `Run SQL and print the last result set. Reads the SQL from stdin when it is
omitted or "-". Ctrl+C cancels a running query.

The connection comes from --url, -c <profile>, or DATABASE_URL / PG* variables,
through an SSH tunnel with --ssh or the profile's ssh setting.
A password saved in the connection history is used when one matches.`,
		Example: `  queryme query -c prod "SELECT count(*) FROM users"
  queryme query --url postgres://app@localhost/app -f csv "SELECT * FROM orders" > orders.csv
  echo "SELECT now()" | queryme query -f json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return c.runQuery(cmd.Context(), opts, args) },
	}
	cmd.Flags().StringVar(&opts.url, "url", "", "connection URL, e.g. postgres://user@host:5432/db")
	cmd.Flags().StringVarP(&opts.format, "format", "f", "table", "output format: "+strings.Join(formatNames(), ", "))
	cmd.Flags().IntVarP(&opts.limit, "limit", "l", 0, fmt.Sprintf("maximum rows to fetch (default ui.row_limit, at most %d)", db.MaxRowLimit))
	_ = cmd.RegisterFlagCompletionFunc("format", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return formatNames(), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func (c *CLI) runQuery(ctx context.Context, opts *queryOptions, args []string) error {
	write, ok := formats[opts.format]
	if !ok {
		return fmt.Errorf("unknown format %q (want %s)", opts.format, strings.Join(formatNames(), ", "))
	}
	if opts.limit < 0 || opts.limit > db.MaxRowLimit {
		return fmt.Errorf("--limit must be between 1 and %d", db.MaxRowLimit)
	}
	sql, err := c.readSQL(args)
	if err != nil {
		return err
	}
	cfg, path, err := c.loadConfig()
	if err != nil {
		return err
	}
	conn, err := c.queryTarget(cfg, path, opts.url)
	if err != nil {
		return err
	}
	limit := cfg.UI.RowLimit
	if opts.limit > 0 {
		limit = opts.limit
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	driver, err := c.open(ctx, conn)
	if err != nil {
		return err
	}
	defer driver.Close()
	res, err := driver.Query(ctx, sql, limit)
	if err != nil {
		return err
	}
	if len(res.Columns) == 0 {
		// INSERT, UPDATE, DDL and the like return no rows, only a command tag such as "UPDATE 3".
		_, err := fmt.Fprintln(c.out, res.Command)
		return err
	}
	if res.Truncated {
		fmt.Fprintf(c.errOut, "queryme: stopped at the %d-row limit; raise it with --limit (max %d)\n", limit, db.MaxRowLimit)
	}
	return write(c.out, res)
}

func (c *CLI) readSQL(args []string) (string, error) {
	sql := ""
	if len(args) == 1 && args[0] != "-" {
		sql = args[0]
	} else if f, ok := c.in.(*os.File); ok && len(args) == 0 && isatty.IsTerminal(f.Fd()) {
		return "", errors.New("no SQL given: pass it as an argument or pipe it on stdin")
	} else {
		data, err := io.ReadAll(c.in)
		if err != nil {
			return "", err
		}
		sql = string(data)
	}
	if strings.TrimSpace(sql) == "" {
		return "", errors.New("no SQL given: pass it as an argument or on stdin")
	}
	return sql, nil
}

func (c *CLI) queryTarget(cfg config.Config, path, url string) (config.Connection, error) {
	switch {
	case url != "":
		return config.Connection{Name: "command line", URL: url}, nil
	case c.connection != "":
		return c.profile(cfg, path)
	}
	if env, ok := config.EnvConnection(c.getenv); ok {
		return env, nil
	}
	return config.Connection{}, errNoConnection
}

// open fills in a password saved in the history, so scripts reuse what the TUI remembered.
func (c *CLI) open(ctx context.Context, conn config.Connection) (*postgres.Driver, error) {
	if c.ssh != "" {
		conn.SSH = c.ssh
	}
	if conn.URL != "" {
		if hist, err := c.openHistory(true); err == nil {
			conn.Password = hist.PasswordFor(conn.URL)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	return postgres.Open(ctx, conn.URL, conn.Password, conn.SSH, conn.ReadOnly)
}
