package db

import (
	"context"
	"time"
)

// MaxRowLimit caps the rows fetched by a single query, whatever the configured limit.
const MaxRowLimit = 10000

// RowLimit keeps a requested limit within 1..MaxRowLimit; zero or negative means the maximum, never unlimited.
func RowLimit(requested int) int {
	if requested <= 0 || requested > MaxRowLimit {
		return MaxRowLimit
	}
	return requested
}

type Driver interface {
	Query(ctx context.Context, sql string, limit int) (*Result, error)
	Schema(ctx context.Context) (*Schema, error)
	Target() string
	Close()
}

// Result holds the last result set of the executed SQL. A nil cell is NULL.
type Result struct {
	Columns    []string
	Rows       [][]*string
	Command    string
	Statements int
	Elapsed    time.Duration
	Truncated  bool
}
