package postgres

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chinhtran52/queryme/internal/db"
	"github.com/chinhtran52/queryme/internal/tunnel"
)

type Driver struct {
	pool   *pgxpool.Pool
	tunnel *tunnel.Tunnel
	target string
}

// Open uses password only when it is non-empty, so a password in the URL or ~/.pgpass still works.
// A non-empty sshHost ("[user@]host[:port]") reaches the database through that SSH server.
func Open(ctx context.Context, url, password, sshHost string, readOnly bool) (*Driver, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	d := &Driver{}
	if sshHost != "" {
		if d.tunnel, err = tunnel.Open(ctx, sshHost); err != nil {
			return nil, err
		}
		cfg.ConnConfig.DialFunc = d.tunnel.Dial
		// The database host name is resolved by the SSH server, so private names work.
		cfg.ConnConfig.LookupFunc = func(_ context.Context, host string) ([]string, error) { return []string{host}, nil }
	}
	if err := d.connect(ctx, cfg, password, readOnly); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *Driver) connect(ctx context.Context, cfg *pgxpool.Config, password string, readOnly bool) error {
	if password != "" {
		cfg.ConnConfig.Password = password
	}
	if readOnly {
		cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	d.pool = pool
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	_, encrypted := conn.Conn().PgConn().Conn().(*tls.Conn)
	conn.Release()
	cc := cfg.ConnConfig
	d.target = fmt.Sprintf("%s@%s:%d/%s", cc.User, cc.Host, cc.Port, cc.Database)
	if d.tunnel != nil {
		d.target += " via " + d.tunnel.Host()
	}
	if encrypted {
		d.target += " · tls"
	}
	return nil
}

func (d *Driver) Target() string { return d.target }

// Close also closes the SSH tunnel; queries fail once the tunnel drops, until the user reconnects.
func (d *Driver) Close() {
	if d.pool != nil {
		d.pool.Close()
	}
	if d.tunnel != nil {
		_ = d.tunnel.Close()
	}
}

// Query uses the simple protocol so a buffer with several statements runs as-is and values arrive as display-ready text.
func (d *Driver) Query(ctx context.Context, sql string, limit int) (*db.Result, error) {
	start := time.Now()
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	limit = db.RowLimit(limit)
	mrr := conn.Conn().PgConn().Exec(ctx, sql)
	res := &db.Result{}
	for mrr.NextResult() {
		rr := mrr.ResultReader()
		res.Statements++
		res.Columns = columnNames(rr.FieldDescriptions())
		res.Rows, res.Truncated = nil, false
		for rr.NextRow() {
			// shortcut: rows past the limit are still read off the wire and discarded; switch to a cursor when huge tables make this slow.
			if len(res.Rows) == limit {
				res.Truncated = true
				break
			}
			res.Rows = append(res.Rows, cells(rr.Values()))
		}
		tag, err := rr.Close()
		if err != nil {
			mrr.Close()
			return nil, err
		}
		res.Command = tag.String()
	}
	if err := mrr.Close(); err != nil {
		return nil, err
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

func columnNames(fields []pgconn.FieldDescription) []string {
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	return names
}

func cells(values [][]byte) []*string {
	row := make([]*string, len(values))
	for i, v := range values {
		if v != nil {
			s := string(v)
			row[i] = &s
		}
	}
	return row
}
