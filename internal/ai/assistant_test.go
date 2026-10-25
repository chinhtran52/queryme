package ai

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chinhtran52/queryme/internal/db"
)

var testSchema = &db.Schema{
	Version: "PostgreSQL 17.6",
	Tables: []db.Table{
		{Schema: "public", Name: "orgs", Columns: []db.Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		{Schema: "public", Name: "users", Columns: []db.Column{
			{Name: "id", Type: "bigint", PrimaryKey: true},
			{Name: "org_id", Type: "integer", References: "public.orgs.id"},
		}},
	},
}

// fakeClaude writes a script that records its arguments and stdin, then prints reply (or sleeps when reply is "sleep").
func fakeClaude(t *testing.T, reply string, exitCode int) (bin, dir string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "reply.json"), []byte(reply), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
d=$(dirname "$0")
for a in "$@"; do printf '%s\n' "$a"; done > "$d/args.txt"
cat > "$d/stdin.txt"
pwd > "$d/cwd.txt"
if [ "$(cat "$d/reply.json")" = sleep ]; then sleep 30; fi
cat "$d/reply.json"
exit ` + string(rune('0'+exitCode)) + "\n"
	bin = filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

func newTestAssistant(bin string, t *testing.T) (*Assistant, string) {
	workDir := filepath.Join(t.TempDir(), "claude-sessions")
	cache := NewSchemaCache(func(context.Context) (*db.Schema, error) { return testSchema, nil })
	return NewAssistant(bin, workDir, cache), workDir
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAskSendsSchemaAndParsesSQL(t *testing.T) {
	bin, dir := fakeClaude(t, `{"type":"result","subtype":"success","is_error":false,"session_id":"s-123",
"result":"Here you go:\n\n`+"```sql\\nSELECT count(*) FROM public.users;\\n```"+`\n\nCounts every user."}`, 0)
	a, workDir := newTestAssistant(bin, t)

	reply, err := a.Ask(context.Background(), Request{Prompt: "count users", Editor: "SELECT 1", LastError: "boom", SessionID: "s-prev"})
	if err != nil {
		t.Fatal(err)
	}
	if reply.SQL != "SELECT count(*) FROM public.users;" || reply.SessionID != "s-123" || reply.Notes != "Here you go:\n\nCounts every user." {
		t.Fatalf("reply = %+v", reply)
	}

	args := read(t, filepath.Join(dir, "args.txt"))
	for _, want := range []string{"-p\n", "--output-format\njson\n", "--tools\n\n", "--resume\ns-prev\n", "--strict-mcp-config\n",
		"PostgreSQL 17.6", "public.users(id bigint pk, org_id integer → public.orgs.id)"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q:\n%s", want, args)
		}
	}
	stdin := read(t, filepath.Join(dir, "stdin.txt"))
	for _, want := range []string{"count users", "```sql\nSELECT 1\n```", "failed with:\nboom"} {
		if !strings.Contains(stdin, want) {
			t.Errorf("prompt missing %q:\n%s", want, stdin)
		}
	}
	if cwd := strings.TrimSpace(read(t, filepath.Join(dir, "cwd.txt"))); cwd != workDir {
		t.Errorf("claude ran in %q, want %q", cwd, workDir)
	}
}

func TestAskErrors(t *testing.T) {
	cases := map[string]struct {
		reply string
		exit  int
		want  string
	}{
		"claude error result": {`{"subtype":"error_during_execution","is_error":true,"result":"Not logged in"}`, 0, "Not logged in"},
		"non-zero exit":       {"Invalid API key", 1, "Invalid API key"},
		"not JSON":            {"hello", 0, "unexpected claude output"},
	}
	for name, c := range cases {
		bin, _ := fakeClaude(t, c.reply, c.exit)
		a, _ := newTestAssistant(bin, t)
		if _, err := a.Ask(context.Background(), Request{Prompt: "x"}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v", name, err)
		}
	}

	a, _ := newTestAssistant(filepath.Join(t.TempDir(), "no-such-claude"), t)
	if _, err := a.Ask(context.Background(), Request{Prompt: "x"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing binary: err = %v", err)
	}
}

func TestAskCancels(t *testing.T) {
	bin, _ := fakeClaude(t, "sleep", 0)
	a, _ := newTestAssistant(bin, t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := a.Ask(ctx, Request{Prompt: "x"}); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
}

func TestSplitReply(t *testing.T) {
	cases := []struct{ in, sql, notes string }{
		{"```sql\nSELECT 1\n```", "SELECT 1", ""},
		{"Try:\n```\nSELECT 2;\n```\nDone.", "SELECT 2;", "Try:\n\nDone."},
		{"```postgresql\r\nSELECT 3\r\n```", "SELECT 3", ""},
		{"The query scans users twice.", "", "The query scans users twice."},
	}
	for _, c := range cases {
		if sql, notes := SplitReply(c.in); sql != c.sql || notes != c.notes {
			t.Errorf("SplitReply(%q) = %q, %q", c.in, sql, notes)
		}
	}
}

func TestRenderSchemaShrinksLargeSchemas(t *testing.T) {
	big := &db.Schema{}
	for i := range 2000 {
		big.Tables = append(big.Tables, db.Table{Schema: "public", Name: "table_" + strconv.Itoa(i),
			Columns: []db.Column{{Name: "some_long_column_name", Type: "character varying(255)"}}})
	}
	big.Tables = append(big.Tables, db.Table{Schema: "public", Name: "invoices", Columns: []db.Column{{Name: "total", Type: "numeric"}}})

	out := RenderSchema(big, "sum of invoices this month")
	if len(out) > maxSchemaChars+len("…") || !strings.Contains(out, "public.invoices(total numeric)") || !strings.Contains(out, "Other tables") {
		t.Fatalf("len=%d, head:\n%.300s", len(out), out)
	}
	if small := RenderSchema(testSchema, "x"); strings.Contains(small, "Other tables") {
		t.Fatalf("small schema should be sent whole:\n%s", small)
	}
}

func TestSchemaCacheLoadsOnceUntilReset(t *testing.T) {
	loads := 0
	c := NewSchemaCache(func(context.Context) (*db.Schema, error) { loads++; return testSchema, nil })
	_, _ = c.Get(context.Background())
	_, _ = c.Get(context.Background())
	c.Reset()
	_, _ = c.Get(context.Background())
	if loads != 2 {
		t.Fatalf("loads = %d, want 2", loads)
	}
}
