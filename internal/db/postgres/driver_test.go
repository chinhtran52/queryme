package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/chinhtran52/queryme/internal/db"
)

func open(t *testing.T, readOnly bool) *Driver {
	t.Helper()
	url := os.Getenv("QUERYME_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("QUERYME_TEST_DATABASE_URL not set")
	}
	d, err := Open(context.Background(), url, "", "", readOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d
}

func TestQueryReturnsLastResultSetWithNulls(t *testing.T) {
	d := open(t, false)
	res, err := d.Query(context.Background(), "SELECT 1; SELECT 'a' AS name, NULL::int AS n", 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Statements != 2 || len(res.Columns) != 2 || res.Columns[0] != "name" {
		t.Fatalf("got %+v", res)
	}
	if *res.Rows[0][0] != "a" || res.Rows[0][1] != nil {
		t.Fatalf("row = %v", res.Rows[0])
	}
}

func TestQueryStopsAtLimit(t *testing.T) {
	d := open(t, false)
	res, err := d.Query(context.Background(), "SELECT g FROM generate_series(1, 50) g", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 10 || !res.Truncated || res.Command != "SELECT 50" {
		t.Fatalf("rows=%d truncated=%v command=%q", len(res.Rows), res.Truncated, res.Command)
	}
}

func TestQueryNeverReturnsMoreThanMaxRowLimit(t *testing.T) {
	d := open(t, false)
	for _, limit := range []int{0, -1, db.MaxRowLimit + 5000} {
		res, err := d.Query(context.Background(), "SELECT g FROM generate_series(1, 12000) g", limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Rows) != db.MaxRowLimit || !res.Truncated {
			t.Errorf("limit %d: rows=%d truncated=%v", limit, len(res.Rows), res.Truncated)
		}
	}
}

func TestQueryErrorAndConnectionReuse(t *testing.T) {
	d := open(t, false)
	if _, err := d.Query(context.Background(), "SELECT * FROM no_such_table", 10); err == nil {
		t.Fatal("expected error")
	}
	if _, err := d.Query(context.Background(), "SELECT 1", 10); err != nil {
		t.Fatalf("pool unusable after error: %v", err)
	}
}

func TestTypedPasswordOverridesURL(t *testing.T) {
	url := os.Getenv("QUERYME_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("QUERYME_TEST_DATABASE_URL not set")
	}
	if d, err := Open(context.Background(), url, "wrong-password", "", false); err == nil {
		d.Close()
		t.Fatal("expected auth failure with the typed password")
	}
}

func TestSchemaListsTablesWithKeys(t *testing.T) {
	d := open(t, false)
	ctx := context.Background()
	setup := `DROP SCHEMA IF EXISTS qm_schema_test CASCADE;
CREATE SCHEMA qm_schema_test;
CREATE TABLE qm_schema_test.orgs (id serial PRIMARY KEY, name text NOT NULL);
CREATE TABLE qm_schema_test.users (id bigint PRIMARY KEY, org_id int REFERENCES qm_schema_test.orgs (id), email varchar(200));`
	if _, err := d.Query(ctx, setup, 1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Query(ctx, "DROP SCHEMA qm_schema_test CASCADE", 1) })

	schema, err := d.Schema(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(schema.Version, "PostgreSQL") {
		t.Errorf("version = %q", schema.Version)
	}
	var users *db.Table
	for i := range schema.Tables {
		if tb := schema.Tables[i]; tb.Schema == "qm_schema_test" && tb.Name == "users" {
			users = &schema.Tables[i]
		}
	}
	if users == nil {
		t.Fatalf("users table missing from %d tables", len(schema.Tables))
	}
	want := []db.Column{
		{Name: "id", Type: "bigint", PrimaryKey: true},
		{Name: "org_id", Type: "integer", References: "qm_schema_test.orgs.id"},
		{Name: "email", Type: "character varying(200)"},
	}
	if fmt.Sprint(users.Columns) != fmt.Sprint(want) {
		t.Fatalf("columns = %+v", users.Columns)
	}
}

func TestReadOnlyBlocksWrites(t *testing.T) {
	d := open(t, true)
	if _, err := d.Query(context.Background(), "CREATE TABLE queryme_ro_check (id int)", 10); err == nil {
		t.Fatal("expected read-only error")
	}
}

func TestTargetSaysWhetherTLSIsUsed(t *testing.T) {
	url := os.Getenv("QUERYME_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("QUERYME_TEST_DATABASE_URL not set")
	}
	d, err := Open(context.Background(), url+"?sslmode=disable", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if strings.Contains(d.Target(), "tls") {
		t.Fatalf("plain connection reported as tls: %q", d.Target())
	}
}
