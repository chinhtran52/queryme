package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chinhtran52/queryme/internal/ai"
	"github.com/chinhtran52/queryme/internal/complete"
	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/db"
	"github.com/chinhtran52/queryme/internal/history"
)

const schemaTimeout = 30 * time.Second

// session holds what every tab of one connection shares.
type session struct {
	driver    db.Driver
	clipboard Clipboard
	// lastCopied is what queryme itself last copied or cut, so paste works without a clipboard tool.
	lastCopied string
	queryLog   *history.QueryLog
	ui         config.UI
	ai         *ai.Assistant
	// schema is loaded once per connection and shared by the AI and autocomplete.
	schema    *ai.SchemaCache
	completer *complete.Completer
}

// schemaLoadedMsg carries the schema for autocomplete; until it arrives, only keywords complete.
type schemaLoadedMsg struct {
	schema *db.Schema
	err    error
}

func loadSchema(cache *ai.SchemaCache) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), schemaTimeout)
		defer cancel()
		schema, err := cache.Get(ctx)
		return schemaLoadedMsg{schema: schema, err: err}
	}
}

// tabMsg is a result that belongs to one tab, so it reaches that tab even after the user switched away.
type tabMsg interface{ tabID() int }

func (m queryDoneMsg) tabID() int { return m.tab }
func (m copyDoneMsg) tabID() int  { return m.tab }
func (m aiDoneMsg) tabID() int    { return m.tab }
func (m aiTickMsg) tabID() int    { return m.tab }
