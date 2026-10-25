package ai

import (
	"context"
	"strings"
	"sync"

	"github.com/chinhtran52/queryme/internal/db"
)

// maxSchemaChars keeps the system prompt well under the 128 KiB limit of a single command-line argument.
const maxSchemaChars = 30000

// SchemaCache loads the schema once per connection and shares it between tabs.
type SchemaCache struct {
	mu     sync.Mutex
	load   func(context.Context) (*db.Schema, error)
	schema *db.Schema
}

func NewSchemaCache(load func(context.Context) (*db.Schema, error)) *SchemaCache {
	return &SchemaCache{load: load}
}

func (c *SchemaCache) Get(ctx context.Context) (*db.Schema, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.schema == nil {
		schema, err := c.load(ctx)
		if err != nil {
			return nil, err
		}
		c.schema = schema
	}
	return c.schema, nil
}

// Reset makes the next Get reload the schema, after DDL changes.
func (c *SchemaCache) Reset() {
	c.mu.Lock()
	c.schema = nil
	c.mu.Unlock()
}

// RenderSchema writes one line per table: "schema.table(col type pk, col type → schema.table.col)".
// A schema too big for the prompt keeps only the tables named in the request, plus a list of the other names.
// shortcut: relevance is a plain name match; rank tables smarter if large schemas give poor answers.
func RenderSchema(schema *db.Schema, request string) string {
	lines := make([]string, len(schema.Tables))
	for i, t := range schema.Tables {
		lines[i] = tableLine(t)
	}
	full := strings.Join(lines, "\n")
	if len(full) <= maxSchemaChars {
		return full
	}

	words := strings.ToLower(request)
	var kept, others []string
	for i, t := range schema.Tables {
		if strings.Contains(words, strings.ToLower(t.Name)) {
			kept = append(kept, lines[i])
		} else {
			others = append(others, t.Schema+"."+t.Name)
		}
	}
	out := strings.Join(kept, "\n") + "\nOther tables (columns not shown): " + strings.Join(others, ", ")
	if len(out) > maxSchemaChars {
		out = out[:maxSchemaChars] + "…"
	}
	return out
}

func tableLine(t db.Table) string {
	cols := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		col := c.Name + " " + c.Type
		if c.PrimaryKey {
			col += " pk"
		}
		if c.References != "" {
			col += " → " + c.References
		}
		cols[i] = col
	}
	return t.Schema + "." + t.Name + "(" + strings.Join(cols, ", ") + ")"
}
