package complete

import (
	"strings"
	"testing"

	"github.com/chinhtran52/queryme/internal/db"
)

var schema = &db.Schema{Tables: []db.Table{
	{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id", Type: "bigint"}, {Name: "email", Type: "text"}, {Name: "org_id", Type: "integer"}}},
	{Schema: "public", Name: "orders", Columns: []db.Column{{Name: "id", Type: "bigint"}, {Name: "user_id", Type: "bigint"}, {Name: "total", Type: "numeric"}}},
	{Schema: "billing", Name: "invoices", Columns: []db.Column{{Name: "invoice_no", Type: "text"}}},
}}

// suggest completes at the "|" marker.
func suggest(t *testing.T, c *Completer, sql string) (string, []string) {
	t.Helper()
	cursor := len([]rune(sql[:strings.Index(sql, "|")]))
	word, items := c.Suggest(strings.Replace(sql, "|", "", 1), cursor)
	texts := make([]string, len(items))
	for i, s := range items {
		texts[i] = s.Text + ":" + s.Kind.String()
	}
	return word, texts
}

func TestSuggest(t *testing.T) {
	c := New(schema)
	cases := []struct {
		name, sql, word string
		want            []string // the first suggestions, in order
	}{
		{"tables after FROM", "SELECT * FROM o|", "o", []string{"orders:table"}},
		{"tables after JOIN on another line", "SELECT *\nFROM users u\nJOIN |", "", nil},
		{"tables after join, lower case", "select * from users join or|", "or", []string{"orders:table"}},
		{"columns through an alias", "SELECT u.e| FROM users u", "e", []string{"email:column"}},
		{"all columns after a dot", "SELECT o.| FROM orders AS o", "", []string{"id:column", "user_id:column", "total:column"}},
		{"columns through a table name", "SELECT users.or| FROM users", "or", []string{"org_id:column"}},
		{"tables of a schema", "SELECT * FROM billing.i|", "i", []string{"invoices:table"}},
		{"query columns before keywords", "SELECT u| FROM orders", "u", []string{"user_id:column", "using:keyword"}},
		{"an exact keyword is not suggested", "SELECT to| FROM orders", "to", []string{"total:column"}},
		{"keywords follow lower case", "sel|", "sel", []string{"select:keyword"}},
		{"keywords follow upper case", "SEL|", "SEL", []string{"SELECT:keyword"}},
		{"an alias that is a keyword is ignored", "SELECT where.| FROM users WHERE id = 1", "", nil},
		{"exact word is not suggested", "SELECT email| FROM users", "email", nil},
		{"nothing at a space", "SELECT |", "", nil},
	}
	for _, tc := range cases {
		word, got := suggest(t, c, tc.sql)
		if word != tc.word {
			t.Errorf("%s: word = %q, want %q", tc.name, word, tc.word)
		}
		if len(got) < len(tc.want) || strings.Join(got[:len(tc.want)], " ") != strings.Join(tc.want, " ") {
			t.Errorf("%s: got %v, want prefix %v", tc.name, got, tc.want)
		}
		if tc.want == nil && len(got) > 0 && tc.name != "tables after JOIN on another line" {
			t.Errorf("%s: expected no suggestions, got %v", tc.name, got)
		}
	}
}

func TestSuggestWithoutSchemaStillCompletesKeywords(t *testing.T) {
	_, got := suggest(t, New(nil), "SELECT * FR|")
	if len(got) != 1 || got[0] != "FROM:keyword" {
		t.Fatalf("got %v", got)
	}
}

func TestSuggestRanksQueryColumnsFirstAndDedupes(t *testing.T) {
	_, got := suggest(t, New(schema), "SELECT i| FROM users")
	// users.id first; orders.id is the same name and appears once; then keywords in the typed (lower) case,
	// then tables, then the other tables' columns.
	want := []string{"id:column", "in:keyword", "is:keyword", "inner:keyword", "insert:keyword", "into:keyword", "index:keyword", "if:keyword", "intersect:keyword", "ilike:keyword", "interval:keyword", "invoices:table", "invoice_no:column"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}
