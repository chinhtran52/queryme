package db

type Column struct {
	Name       string
	Type       string
	PrimaryKey bool
	References string // "schema.table.column" for a foreign key, else ""
}

type Table struct {
	Schema  string
	Name    string
	Columns []Column
}

// Schema is what the AI needs to know about the database: its version and the user tables.
type Schema struct {
	Version string
	Tables  []Table
}
