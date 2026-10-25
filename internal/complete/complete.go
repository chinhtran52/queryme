package complete

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/chinhtran52/queryme/internal/db"
)

// Keywords is the SQL vocabulary shared by completion and highlighting.
var Keywords = strings.Fields(`SELECT FROM WHERE AND OR NOT IN IS NULL AS ON USING JOIN LEFT RIGHT INNER OUTER FULL CROSS
LATERAL NATURAL GROUP BY ORDER HAVING LIMIT OFFSET FETCH FIRST NEXT ROWS ROW ONLY TIES INSERT INTO VALUES UPDATE
SET DELETE RETURNING CREATE TABLE ALTER DROP TRUNCATE INDEX VIEW MATERIALIZED SCHEMA SEQUENCE ADD COLUMN CONSTRAINT
PRIMARY FOREIGN KEY REFERENCES UNIQUE CHECK DEFAULT IF EXISTS CASCADE WITH RECURSIVE UNION INTERSECT EXCEPT ALL
DISTINCT CASE WHEN THEN ELSE END BETWEEN LIKE ILIKE SIMILAR ASC DESC NULLS TRUE FALSE BEGIN COMMIT ROLLBACK
TRANSACTION EXPLAIN ANALYZE VERBOSE OVER PARTITION WINDOW FILTER WITHIN CAST INTERVAL ANY SOME GRANT REVOKE TO`)

var keywordSet = func() map[string]bool {
	set := map[string]bool{}
	for _, k := range Keywords {
		set[k] = true
	}
	return set
}()

type Kind int

const (
	KindColumn Kind = iota
	KindTable
	KindSchema
	KindKeyword
)

func (k Kind) String() string {
	return [...]string{"column", "table", "schema", "keyword"}[k]
}

type Suggestion struct {
	Text   string
	Kind   Kind
	Detail string // the table of a column, the schema of a table
}

// Completer suggests names from one database schema; a nil schema still completes keywords.
type Completer struct {
	tables []db.Table
}

func New(schema *db.Schema) *Completer {
	c := &Completer{}
	if schema != nil {
		c.tables = schema.Tables
	}
	return c
}

var (
	partialWord = regexp.MustCompile(`(?:([A-Za-z_][A-Za-z0-9_]*)\.)?([A-Za-z_][A-Za-z0-9_]*)?$`)
	tableRef    = regexp.MustCompile(`(?i)\b(?:from|join|update|into)\s+([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?)(?:\s+(?:as\s+)?([A-Za-z_][A-Za-z0-9_]*))?`)
	lastWord    = regexp.MustCompile(`([A-Za-z_]+)\s+$`)
	tableWords  = map[string]bool{"FROM": true, "JOIN": true, "UPDATE": true, "INTO": true, "TABLE": true}
)

// Suggest returns the partial word just before cursor (a rune offset into text) and its candidates, best first.
// shortcut: references come from FROM/JOIN/UPDATE/INTO with one optional alias; comma-separated FROM lists and
// subquery aliases are not resolved. Parse properly if suggestions miss in real queries.
func (c *Completer) Suggest(text string, cursor int) (word string, items []Suggestion) {
	runes := []rune(text)
	before := string(runes[:min(max(cursor, 0), len(runes))])
	m := partialWord.FindStringSubmatch(before)
	qualifier, word := m[1], m[2]
	if qualifier == "" && word == "" {
		return "", nil
	}

	refs := references(text)
	var candidates []Suggestion
	switch {
	case qualifier != "":
		candidates = c.qualified(qualifier, refs)
	case tableWords[strings.ToUpper(lastWordOf(strings.TrimSuffix(before, word)))]:
		candidates = c.tableCandidates()
	default:
		candidates = append(c.columnsOf(refs), keywordCandidates(word)...)
		candidates = append(candidates, c.tableCandidates()...)
		candidates = append(candidates, c.columnsOf(nil)...)
	}
	return word, filter(candidates, word)
}

func lastWordOf(s string) string {
	if m := lastWord.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// references maps each alias and table name in the query to the table it names.
func references(text string) map[string]string {
	refs := map[string]string{}
	for _, m := range tableRef.FindAllStringSubmatch(text, -1) {
		table := strings.ToLower(m[1])
		refs[table] = table
		refs[table[strings.LastIndex(table, ".")+1:]] = table
		if alias := strings.ToLower(m[2]); alias != "" && !keywordSet[strings.ToUpper(alias)] {
			refs[alias] = table
		}
	}
	return refs
}

func (c *Completer) qualified(qualifier string, refs map[string]string) []Suggestion {
	q := strings.ToLower(qualifier)
	var out []Suggestion
	for _, t := range c.tables {
		if strings.ToLower(t.Schema) == q {
			out = append(out, Suggestion{Text: t.Name, Kind: KindTable, Detail: t.Schema})
		}
	}
	if len(out) > 0 {
		return out
	}
	target := q
	if ref, ok := refs[q]; ok {
		target = ref
	}
	return c.columnsOf(map[string]string{target: target})
}

// columnsOf lists the columns of the referenced tables, or of every table when refs is nil.
func (c *Completer) columnsOf(refs map[string]string) []Suggestion {
	var out []Suggestion
	for _, t := range c.tables {
		if refs != nil && !matchesRef(t, refs) {
			continue
		}
		for _, col := range t.Columns {
			out = append(out, Suggestion{Text: col.Name, Kind: KindColumn, Detail: t.Name + " · " + col.Type})
		}
	}
	return out
}

func matchesRef(t db.Table, refs map[string]string) bool {
	full := strings.ToLower(t.Schema + "." + t.Name)
	for _, ref := range refs {
		if ref == full || (!strings.Contains(ref, ".") && ref == strings.ToLower(t.Name)) {
			return true
		}
	}
	return false
}

func (c *Completer) tableCandidates() []Suggestion {
	var out []Suggestion
	schemas := map[string]bool{}
	for _, t := range c.tables {
		out = append(out, Suggestion{Text: t.Name, Kind: KindTable, Detail: t.Schema})
		schemas[t.Schema] = true
	}
	for _, t := range c.tables {
		if schemas[t.Schema] {
			out = append(out, Suggestion{Text: t.Schema, Kind: KindSchema})
			delete(schemas, t.Schema)
		}
	}
	return out
}

// keywordCandidates follows the case being typed: "sel" completes to "select", "SEL" or "Sel" to "SELECT".
func keywordCandidates(word string) []Suggestion {
	lower := word != "" && strings.IndexFunc(word, unicode.IsUpper) < 0
	out := make([]Suggestion, len(Keywords))
	for i, k := range Keywords {
		if lower {
			k = strings.ToLower(k)
		}
		out[i] = Suggestion{Text: k, Kind: KindKeyword}
	}
	return out
}

// filter keeps candidates that extend word, case-insensitively, dropping exact matches and repeats.
func filter(candidates []Suggestion, word string) []Suggestion {
	prefix := strings.ToLower(word)
	seen := map[string]bool{}
	var out []Suggestion
	for _, s := range candidates {
		lower := strings.ToLower(s.Text)
		if !strings.HasPrefix(lower, prefix) || lower == prefix || seen[lower] {
			continue
		}
		seen[lower] = true
		out = append(out, s)
	}
	return out
}
