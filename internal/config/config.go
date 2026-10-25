package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/chinhtran52/queryme/internal/db"
)

type Connection struct {
	Name     string `toml:"name"`
	URL      string `toml:"url"`
	ReadOnly bool   `toml:"read_only"`
	// SSH is "[user@]host[:port]" of a server to tunnel through; empty connects directly.
	SSH string `toml:"ssh"`
	// Password is typed at connect time and never written to the config file.
	Password string `toml:"-"`
}

type UI struct {
	MaxColWidth int `toml:"max_col_width"`
	RowLimit    int `toml:"row_limit"`
}

// AI configures the claude CLI bridge.
type AI struct {
	ClaudeBin string `toml:"claude_bin"`
}

type Config struct {
	UI          UI           `toml:"ui"`
	AI          AI           `toml:"ai"`
	Connections []Connection `toml:"connections"`
}

func Default() Config {
	return Config{UI: UI{MaxColWidth: 40, RowLimit: 1000}, AI: AI{ClaudeBin: "claude"}}
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "queryme", "config.toml"), nil
}

// Load returns the defaults when the file does not exist yet.
func Load(path string) (Config, error) {
	cfg := Default()
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if c.UI.MaxColWidth < 1 {
		return errors.New("ui.max_col_width must be positive")
	}
	if c.AI.ClaudeBin == "" {
		return errors.New("ai.claude_bin must not be empty")
	}
	if c.UI.RowLimit < 1 || c.UI.RowLimit > db.MaxRowLimit {
		return fmt.Errorf("ui.row_limit must be between 1 and %d", db.MaxRowLimit)
	}
	seen := map[string]bool{}
	for i, conn := range c.Connections {
		if conn.Name == "" || conn.URL == "" {
			return fmt.Errorf("connection #%d needs both name and url", i+1)
		}
		if seen[conn.Name] {
			return fmt.Errorf("duplicate connection name %q", conn.Name)
		}
		seen[conn.Name] = true
	}
	return nil
}

func (c Config) Find(name string) (Connection, bool) {
	for _, conn := range c.Connections {
		if conn.Name == name {
			return conn, true
		}
	}
	return Connection{}, false
}

// EnvConnection returns the connection described by DATABASE_URL or the PG* variables, if any.
func EnvConnection(getenv func(string) string) (Connection, bool) {
	switch {
	case getenv("DATABASE_URL") != "":
		return Connection{Name: "env (DATABASE_URL)", URL: getenv("DATABASE_URL")}, true
	case getenv("PGHOST") != "" || getenv("PGDATABASE") != "" || getenv("PGSERVICE") != "":
		// An empty URL makes pgx read the PG* variables itself.
		return Connection{Name: "env (PG* variables)"}, true
	}
	return Connection{}, false
}

// Candidates lists the picker entries: the environment connection first (when set), then saved profiles.
func (c Config) Candidates(getenv func(string) string) []Connection {
	var out []Connection
	if env, ok := EnvConnection(getenv); ok {
		out = append(out, env)
	}
	return append(out, c.Connections...)
}
