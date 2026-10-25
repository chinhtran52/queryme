package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI != Default().UI {
		t.Fatalf("got %+v, want defaults", cfg.UI)
	}
}

func TestLoadKeepsDefaultsForMissingKeys(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[ui]
row_limit = 50

[[connections]]
name = "prod"
url = "postgres://app@db/app"
read_only = true
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.RowLimit != 50 || cfg.UI.MaxColWidth != 40 {
		t.Fatalf("ui = %+v", cfg.UI)
	}
	if conn, ok := cfg.Find("prod"); !ok || !conn.ReadOnly {
		t.Fatalf("prod = %+v, %v", conn, ok)
	}
}

func TestLoadRejectsInvalidConnections(t *testing.T) {
	for name, body := range map[string]string{
		"missing url":  "[[connections]]\nname = \"a\"",
		"duplicate":    "[[connections]]\nname = \"a\"\nurl = \"x\"\n[[connections]]\nname = \"a\"\nurl = \"y\"",
		"bad limit":    "[ui]\nrow_limit = 0",
		"over max":     "[ui]\nrow_limit = 10001",
		"empty claude": "[ai]\nclaude_bin = \"\"",
	} {
		if _, err := Load(writeConfig(t, body)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestCandidatesPutsEnvFirst(t *testing.T) {
	cfg := Config{Connections: []Connection{{Name: "prod", URL: "x"}}}
	env := map[string]string{"PGHOST": "localhost"}

	got := cfg.Candidates(func(k string) string { return env[k] })
	if len(got) != 2 || got[0].URL != "" || got[1].Name != "prod" {
		t.Fatalf("got %+v", got)
	}

	env["DATABASE_URL"] = "postgres://u@h/db"
	if got := cfg.Candidates(func(k string) string { return env[k] }); got[0].URL != "postgres://u@h/db" {
		t.Fatalf("DATABASE_URL should win, got %+v", got[0])
	}

	if got := cfg.Candidates(func(string) string { return "" }); len(got) != 1 {
		t.Fatalf("no env: got %+v", got)
	}
}
