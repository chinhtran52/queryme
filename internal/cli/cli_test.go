package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/history"
	"github.com/chinhtran52/queryme/internal/secret"
)

// run executes the CLI with an isolated config, data dir and keyring, and no env connection.
func run(t *testing.T, stdin string, env map[string]string, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	c := New(strings.NewReader(stdin), &out, &errOut)
	c.getenv = func(k string) string { return env[k] }
	c.latest = func(context.Context) (string, error) { return "", errors.New("offline in tests") }
	err := c.Execute(args)
	return out.String(), errOut.String(), err
}

func isolate(t *testing.T, configBody string) string {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHelpListsCommands(t *testing.T) {
	out, _, err := run(t, "", nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"query", "connections", "version", "completion", "--connection"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

func TestVersion(t *testing.T) {
	version = "v1.2.3"
	t.Cleanup(func() { version = "dev" })
	if out, _, err := run(t, "", nil, "version"); err != nil || out != "queryme v1.2.3\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestQueryValidatesBeforeConnecting(t *testing.T) {
	cfg := isolate(t, "")
	cases := map[string][]string{
		"unknown format":   {"query", "--config", cfg, "-f", "xml", "SELECT 1"},
		"limit over max":   {"query", "--config", cfg, "-l", "10001", "SELECT 1"},
		"no SQL":           {"query", "--config", cfg},
		"no connection":    {"query", "--config", cfg, "SELECT 1"},
		"unknown profile":  {"query", "--config", cfg, "-c", "nope", "SELECT 1"},
		"too many args":    {"query", "--config", cfg, "SELECT 1", "SELECT 2"},
		"tui unknown prof": {"--config", cfg, "-c", "nope"},
	}
	for name, args := range cases {
		if _, _, err := run(t, "", nil, args...); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestConnectionsListHidesPasswords(t *testing.T) {
	cfg := isolate(t, `
[[connections]]
name = "prod"
url = "postgres://app:TOPSECRET@db:5432/app"
read_only = true
[[connections]]
name = "kv"
url = "host=db user=app password='TOP SECRET'"
`)
	key := make([]byte, 32)
	cipher, _ := secret.NewCipher(key)
	dataDir, _ := history.DefaultDir()
	hist, _ := history.Open(filepath.Join(dataDir, "connections.json"), cipher)
	if err := hist.Record(config.Connection{URL: "postgres://me@localhost/db", Password: "HISTSECRET"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{"DATABASE_URL": "postgres://u:ENVSECRET@h/db"}
	for _, format := range []string{"table", "csv", "tsv", "json"} {
		out, _, err := run(t, "", env, "connections", "list", "--config", cfg, "-f", format)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "SECRET") {
			t.Errorf("%s output leaks a password:\n%s", format, out)
		}
		for _, want := range []string{"env", "prod", "postgres://app@db:5432/app", "password=…", "postgres://me@localhost/db"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s output missing %q:\n%s", format, want, out)
			}
		}
	}
}

func TestQueryAgainstPostgres(t *testing.T) {
	url := os.Getenv("QUERYME_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("QUERYME_TEST_DATABASE_URL not set")
	}
	cfg := isolate(t, "")
	sql := "SELECT g AS id, CASE WHEN g = 2 THEN NULL ELSE 'a,b' END AS note FROM generate_series(1, 3) g"

	cases := map[string]struct {
		args []string
		want string
	}{
		"table": {[]string{"--url", url, sql}, " id | note\n----+------\n 1  | a,b\n 2  | NULL\n 3  | a,b\n(3 rows)\n"},
		"csv":   {[]string{"--url", url, "-f", "csv", sql}, "id,note\n1,\"a,b\"\n2,\n3,\"a,b\"\n"},
		"tsv":   {[]string{"--url", url, "-f", "tsv", "-l", "1", sql}, "id\tnote\n1\ta,b\n"},
		"json":  {[]string{"--url", url, "-f", "json", "-l", "2", sql}, "[\n  {\n    \"id\": \"1\",\n    \"note\": \"a,b\"\n  },\n  {\n    \"id\": \"2\",\n    \"note\": null\n  }\n]\n"},
		"dml":   {[]string{"--url", url, "CREATE TEMP TABLE t (i int); INSERT INTO t VALUES (1), (2)"}, "INSERT 0 2\n"},
	}
	for name, c := range cases {
		out, errOut, err := run(t, "", nil, append([]string{"query", "--config", cfg}, c.args...)...)
		if err != nil || out != c.want {
			t.Errorf("%s: err=%v stderr=%q\ngot:\n%s\nwant:\n%s", name, err, errOut, out, c.want)
		}
	}

	out, errOut, err := run(t, "SELECT 42 AS answer", map[string]string{"DATABASE_URL": url}, "query", "--config", cfg, "-f", "tsv", "-l", "1")
	if err != nil || out != "answer\n42\n" || errOut != "" {
		t.Fatalf("stdin + env: out=%q stderr=%q err=%v", out, errOut, err)
	}

	if _, _, err := run(t, "", nil, "query", "--config", cfg, "--url", url, "SELECT * FROM no_such_table"); err == nil {
		t.Fatal("SQL errors must fail the command")
	}
}

func TestConnectionsForget(t *testing.T) {
	cfg := isolate(t, "")
	dataDir, _ := history.DefaultDir()
	path := filepath.Join(dataDir, "connections.json")
	hist, _ := history.Open(path, nil)
	for _, url := range []string{"postgres://me@localhost/keep", "postgres://me@localhost/drop"} {
		if err := hist.Record(config.Connection{URL: url}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	if out, _, err := run(t, "", nil, "connections", "forget", "--config", cfg, "postgres://me:pw@localhost/drop"); err != nil || !strings.Contains(out, "Forgot postgres://me@localhost/drop") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, _, err := run(t, "", nil, "connections", "rm", "--config", cfg, "postgres://me@localhost/drop"); err == nil {
		t.Fatal("forgetting twice should fail")
	}
	reopened, _ := history.Open(path, nil)
	if got := reopened.Entries(); len(got) != 1 || got[0].URL != "postgres://me@localhost/keep" {
		t.Fatalf("entries = %+v", got)
	}
}

func TestSSHFlag(t *testing.T) {
	cfg := isolate(t, `
[[connections]]
name = "prod"
url = "postgres://app@db.internal/app"
ssh = "me@bastion"
`)
	if _, _, err := run(t, "", nil, "--config", cfg, "--ssh", "me@bastion"); err == nil || !strings.Contains(err.Error(), "--ssh needs") {
		t.Fatalf("--ssh without a connection: %v", err)
	}

	out, _, err := run(t, "", nil, "connections", "list", "--config", cfg, "-f", "csv")
	if err != nil || !strings.Contains(out, "source,name,url,ssh,") || !strings.Contains(out, "postgres://app@db.internal/app,me@bastion,") {
		t.Fatalf("list:\n%s err=%v", out, err)
	}

	dataDir, _ := history.DefaultDir()
	hist, _ := history.Open(filepath.Join(dataDir, "connections.json"), nil)
	if err := hist.Record(config.Connection{URL: "postgres://app@db/app", SSH: "me@bastion"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, "", nil, "connections", "forget", "--config", cfg, "postgres://app@db/app"); err == nil {
		t.Fatal("forget without --ssh must not match a tunnelled entry")
	}
	if out, _, err := run(t, "", nil, "connections", "forget", "--config", cfg, "--ssh", "me@bastion", "postgres://app@db/app"); err != nil || !strings.Contains(out, "via me@bastion") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestConnectionsRenameAndNameFlag(t *testing.T) {
	cfg := isolate(t, "")
	if _, _, err := run(t, "", nil, "--config", cfg, "--name", "x"); err == nil || !strings.Contains(err.Error(), "--name needs") {
		t.Fatalf("--name without a URL: %v", err)
	}

	dataDir, _ := history.DefaultDir()
	path := filepath.Join(dataDir, "connections.json")
	hist, _ := history.Open(path, nil)
	if err := hist.Record(config.Connection{URL: "postgres://me@localhost/db"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	if _, _, err := run(t, "", nil, "connections", "rename", "--config", cfg, "postgres://me:pw@localhost/db", "local dev"); err != nil {
		t.Fatal(err)
	}
	out, _, _ := run(t, "", nil, "connections", "list", "--config", cfg, "-f", "csv")
	if !strings.Contains(out, "recent,local dev,postgres://me@localhost/db") {
		t.Fatalf("list:\n%s", out)
	}
	if _, _, err := run(t, "", nil, "connections", "rename", "--config", cfg, "postgres://me@localhost/other", "x"); err == nil {
		t.Fatal("renaming an unknown connection should fail")
	}
	if _, _, err := run(t, "", nil, "connections", "rename", "--config", cfg, "postgres://me@localhost/db"); err != nil {
		t.Fatal(err)
	}
	if reopened, _ := history.Open(path, nil); reopened.Entries()[0].Name != "" {
		t.Fatalf("name not cleared: %+v", reopened.Entries()[0])
	}
}

func TestUpdateCheckNoticesANewerRelease(t *testing.T) {
	version = "v1.0.0"
	t.Cleanup(func() { version = "dev" })
	latest := func(context.Context) (string, error) { return "v1.1.0", nil }
	cases := map[string]struct {
		env  map[string]string
		args []string
		want string
	}{
		"newer":         {nil, []string{"version"}, "v1.1.0"},
		"opted out":     {map[string]string{"QUERYME_NO_UPDATE_CHECK": "1"}, []string{"version"}, ""},
		"update itself": {nil, []string{"update"}, ""},
		"completion":    {nil, []string{"__complete", "-c", ""}, ""},
	}
	for name, tc := range cases {
		c := New(nil, &bytes.Buffer{}, &bytes.Buffer{})
		c.getenv = func(k string) string { return tc.env[k] }
		c.latest = latest
		got := ""
		select {
		case got = <-c.checkForUpdate(tc.args):
		case <-time.After(200 * time.Millisecond):
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestUpdateInstallsOnlyWhenNewer(t *testing.T) {
	version = "v1.0.0"
	t.Cleanup(func() { version = "dev" })
	for latest, wantInstall := range map[string]bool{"v1.0.0": false, "v1.0.1": true} {
		var out bytes.Buffer
		c := New(nil, &out, &bytes.Buffer{})
		c.getenv = func(string) string { return "" }
		c.latest = func(context.Context) (string, error) { return latest, nil }
		installed := ""
		c.install = func(_ context.Context, v, exe string) error {
			installed = v
			return nil
		}
		if err := c.Execute([]string{"update"}); err != nil {
			t.Fatal(err)
		}
		if (installed == latest) != wantInstall {
			t.Errorf("latest %s: installed %q, output %q", latest, installed, out.String())
		}
	}
}
