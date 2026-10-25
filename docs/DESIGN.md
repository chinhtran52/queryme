# queryme — Plan & Design

An AI-first database client for the terminal. Start it with `queryme`.

## 1. Decisions

| Topic | Decision |
|---|---|
| Language / TUI | Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea), Lip Gloss, bubbles (textarea, table, textinput) |
| Postgres driver | [pgx v5](https://github.com/jackc/pgx) (`pgxpool`) |
| AI | Shell out to the user's `claude` CLI (`claude -p`), which reuses their Claude Code login. No API key |
| AI safety | The AI **only inserts SQL into the editor**. It never runs anything. The user always presses Run |
| Connections | Saved profiles in a config file, plus DSN/env vars, plus a connection picker on start |
| Databases | PostgreSQL only for v1. Others are in the TODO list (§10) |
| Distribution | Single static binary (`go install`, then GoReleaser later) |

Go has no classes, so the code follows the OOP style through **structs with methods behind small interfaces**. Each component (Tab, Editor, ResultTable, AIAssistant, Driver) owns its state and behaviour.

## 2. Startup flow and commands

The command line is built with cobra (`internal/cli`), so every command has `--help`, and `queryme completion bash|zsh|fish|powershell` prints shell completion, which also completes `-c` profile names.

```
queryme                       → TUI: picker of recent, env and saved connections
queryme postgres://u@h/db     → TUI connected to that URL (--name to remember it by a name)
queryme -c prod               → TUI connected to the saved profile "prod"
queryme --ssh me@bastion URL  → any connection through an SSH tunnel (also on query, connections forget)
queryme query [sql]           → run SQL (or stdin) and print it: -f table|csv|tsv|json, -l limit, --url
queryme connections list      → env, saved and recent connections (-f as above); passwords never shown
queryme connections forget <url> → remove a recent connection and its saved password (alias rm)
queryme connections rename <url> [name] → name a recent connection, or clear its name
queryme version / --version   → version stamped at build time
queryme update                → download the latest release, check its sha256, replace this binary
--config <file>               → any command: use another config file
```

Precedence for the TUI: URL argument > `-c profile` > picker. For `query`: `--url` > `-c profile` > `DATABASE_URL` / `PG*`. `query` uses a password saved in the connection history for the same URL, unless the URL carries its own password. Errors go to stderr with exit code 1, and a result cut at the row limit warns on stderr. Ctrl+C cancels a running `query`.

**Install and release.** `make install` builds with the git version stamped in and installs to `~/.local/bin/queryme` (`PREFIX=` to change). `make test-db` runs the Postgres-backed tests against `make db-up`. Pushing a `v*` tag runs `.github/workflows/release.yml`, where GoReleaser builds static (`CGO_ENABLED=0`) Linux and macOS binaries for amd64 and arm64. `make snapshot` builds the same archives locally into `dist/` without publishing.

**Update check.** Every run except `update`, `completion` and dev builds asks GitHub in the background for the tag that `/releases/latest` redirects to (no API rate limit, 3 s timeout). If the command finishes after the answer arrives and the tag is newer, a notice goes to stderr; the run never waits for it. `QUERYME_NO_UPDATE_CHECK=1` turns it off. `queryme update` (`internal/update`) downloads the archive for this OS/arch, checks it against `checksums.txt`, and renames it over the running binary from a temp file in the same directory.

The picker lists, in order:
1. **Recent:** the last 10 successful connections, newest first. The cursor starts on the newest one, so Enter reconnects.
2. **Saved:** the env connection, then the config profiles.
3. **New connection…:** asks for a URL, then an optional SSH tunnel host, an optional name, then a password with hidden input.

Recent connections live in `~/.local/share/queryme/connections.json` (or `$XDG_DATA_HOME`), with mode 0600.
- **Passwords are saved encrypted** with AES-256-GCM, never in plain text. Each ciphertext is bound to its URL, so it can't be moved to another entry. They are stripped from both the URL and its `?password=` parameter first.
- **Key:** 32 random bytes in the OS keyring (GNOME Keyring / KWallet via Secret Service, or the macOS Keychain), under service `queryme`, user `history-key`.
- **No keyring** (SSH, headless): passwords aren't saved. The password prompt says so and asks on every reconnect. A password saved earlier from a session with a keyring is kept, not wiped.
- **Not app-only:** any process running as the same user can still reach the keyring. Encryption protects the file at rest (copies, backups, a stolen disk).
- **Reconnect** uses the saved password directly. If it's missing or can't be decrypted (lost key, tampered file), or the connection fails, the password prompt appears with the URL filled in.
- **Name:** optional. A named recent connection shows its name with the URL dimmed beside it; `r` on a Recent row renames it, and an empty name shows the URL again. Reconnecting without a name keeps the one set before. Saved profiles are recorded under their profile name.
- **Forget:** `d` on a Recent row asks for confirmation, then `y` removes it and its saved password from the file. Saved profiles are edited in `config.toml`.
- A read-only connection stays read-only on reconnect and on retry. Keyword DSNs (`host=… user=…`) and env-only connections aren't remembered.

**SSL** is set in the URL with the libpq parameters pgx understands: `sslmode` (`disable`, `prefer` (default), `require`, `verify-ca`, `verify-full`), `sslrootcert`, `sslcert`, `sslkey` and `sslpassword`, e.g. `postgres://app@db/app?sslmode=verify-full&sslrootcert=/home/me/ca.pem`. File paths must be absolute: `~` is not expanded. The header shows `· tls` when the session is encrypted. An `sslpassword` (the client key passphrase) is kept out of the history URL and saved encrypted like the password, bound to its URL, and never shown in the picker.

**SSH tunnel:** `ssh = "[user@]host[:port]"` on a profile, `--ssh` on the command line, or the SSH step in the picker. queryme dials the database through the SSH connection itself (no local port, no `ssh` child process), and the SSH server resolves the database host, so private names like `db.internal` work. TLS still checks the real database host name. The header shows `via <host>`, and a recent connection keeps its tunnel (URL + tunnel identify an entry).
- **Auth:** keys from `ssh-agent` (`SSH_AUTH_SOCK`), then unencrypted `~/.ssh/id_ed25519`, `id_ecdsa`, `id_rsa`. The user defaults to the local user, the port to 22.
- **Host keys:** only keys already in `~/.ssh/known_hosts` are trusted, and only the key types listed there are requested. An unknown host fails with a hint to run `ssh` once; a changed key refuses to connect.
- A keepalive every 30s keeps idle sessions from being dropped. If the tunnel dies, queries fail until you reconnect.
- Not supported: `~/.ssh/config` host aliases and options, ProxyJump chains, password auth, passphrase keys without the agent.

## 3. Screen layout

```
┌ queryme ─ prod (postgres@db.example:5432/app) ─────────────────────────────┐
│ [1 users*] [2 orders] [3 AI: top customers] [+]                            │
├────────────────────────────────────────────────────────────────────────────┤
│  1 SELECT id, email, created_at, profile                                   │
│  2 FROM users                                                              │
│  3 WHERE created_at > now() - interval '7 days';                           │
│                                                                   EDITOR   │
├────────────────────────────────────────────────────────────────────────────┤
│ ✦ Ask AI  ctrl+k · enter to send · esc back                                │
│ top 5 customers by order value_                                            │
│                                                                            │
│                                                                AI PROMPT   │
├────────────────────────────────────────────────────────────────────────────┤
│ id │ email                │ created_at          │ profile                  │
│────┼──────────────────────┼─────────────────────┼──────────────────────────│
│  1 │ ann@example.com      │ 2026-10-20 09:12:01 │ {"theme":"dark","lang…   │
│▶ 2 │ bob.longername@exam… │ 2026-10-21 14:40:55 │ {"theme":"light","la…    │
│  3 │ cy@example.com       │ 2026-10-22 08:03:17 │ NULL                     │
│                                                                   RESULTS  │
├────────────────────────────────────────────────────────────────────────────┤
│ 3 rows · 12 ms · row 2/3            ^R run ^K ask ^T tab ^W close ? help   │
└────────────────────────────────────────────────────────────────────────────┘
```

The row action menu opens on Enter or a right click on a row:

```
        ┌ Row 2 ───────────────┐
        │ ▶ Copy row (TSV)     │
        │   Copy row (JSON)    │
        │   Copy cell          │
        │   View full row      │
        └──────────────────────┘
```

"View full row" opens a modal that shows every column untruncated, with pretty-printed JSON. It exists because truncation hides values.

## 4. Keybindings

| Key | Action |
|---|---|
| `Ctrl+R` / `F5` | Run the editor buffer (or the selection) |
| `Ctrl+C` while running | Cancel the active tab's query or AI request (context cancel → pgx sends a cancel request); quits when idle |
| `Ctrl+K` | Focus the AI prompt; `Enter` asks, `Esc` returns to the editor |
| `Ctrl+K` | Focus the AI prompt line |
| `Ctrl+T` / `Ctrl+W` | New tab / close tab |
| `Ctrl+Shift+1..9`, `Ctrl+PgUp/PgDn` | Switch tab (mouse click on a tab also works; `Alt+1..9` where the terminal sends it) |
| `Tab` / `Shift+Tab` | Cycle focus: editor → AI prompt → results, or the reverse (in the editor, `Tab` first accepts an open suggestion) |
| `Ctrl+J` | Jump to the current tab's SQL editor from anywhere, closing an open row menu |
| `Ctrl+D` / `Ctrl+X` (editor) | Duplicate the current line / cut it, with its newline, to the clipboard |
| `Ctrl+V` (editor) | Paste at the cursor from the system clipboard; without a tool (or over SSH) it pastes queryme's own last copy or cut |
| `↑↓←→` / `hjkl`, `PgUp/PgDn`, `Home/End` (`g`/`G`), `0`/`$` | Move the cell cursor in results; the grid scrolls by column to keep it visible |
| Left click | Focus the editor or results; on a cell, also select it |
| Double click on a cell | Copy that cell's value as text (NULL copies as empty); two presses on one cell within 400 ms, since terminals only report single presses |
| `Enter` / right click on a row | Row action menu (`↑↓` + `Enter`, a left click on an item, or `Esc` / a click outside to close) |
| Mouse wheel | Move through result rows; scroll the full-row viewer |
| `Esc` in results | Back to the editor |
| `y` / `Y` in results | Quick copy cell / row (TSV) |
| `F1`, or `?` in results | Help overlay listing every key; any key or click closes it |
| `Ctrl+Q` | Quit |

`Ctrl+Enter` is avoided on purpose because most terminals can't tell it apart from `Enter`.

## 5. Architecture

```
cmd/queryme/main.go          flags, config load, start tea.Program (alt-screen + mouse)
internal/
  app/        App            root tea.Model: tab bar, focus, global keys, status bar
              Tab            one query session: Editor + AIPrompt + ResultTable + history
  ui/         Editor         wraps bubbles/textarea, SQL keyword highlighting (later)
              ResultTable    custom grid: truncation, h-scroll, row cursor, mouse hit-test
              RowMenu        modal action list
              RowViewer      full-row modal
              Picker         connection picker / new-connection form
  db/         Driver         interface (see below); postgres/ implements it
              postgres.Driver  pgxpool, Query, Cancel, Schema introspection
  ai/         Assistant      builds the prompt, runs `claude -p`, parses SQL out of the reply
              SchemaCache    cached table/column summary per connection
  config/     Store          load/save connections.toml, keyring lookups
  clip/       Clipboard      system clipboard with OSC 52 fallback
```

### Driver interface (the seam for future databases)

```go
type Driver interface {
    Query(ctx context.Context, sql string, limit int) (*Result, error)
    Target() string                             // user@host:port/db, never the password
    Close()
    // M4 adds: Schema(ctx) (*Schema, error) and Dialect() string for the AI prompt
}

type Result struct {
    Columns    []string
    Rows       [][]*string // nil = NULL
    Command    string      // command tag, e.g. "SELECT 50", "UPDATE 3"
    Statements int
    Elapsed    time.Duration
    Truncated  bool        // hit the row limit
}
```

The Postgres driver uses the simple query protocol, so a multi-statement buffer runs as-is and values arrive as display-ready text. Nothing gets re-formatted.

### Concurrency model

Every slow operation (connect, query, AI call, schema load) runs as a `tea.Cmd` and returns a message tagged with its `tabID`. Each tab can therefore have its own query or AI call in flight, and switching tabs never blocks. Each running operation keeps a `context.CancelFunc` on its Tab.

## 6. Query execution

- The whole buffer runs, or the selection if there is one. If several statements run, the **last** result set is shown and the status bar reports "N statements".
- Row cap: by default the first **1,000** rows are fetched (`Result.Truncated` → status bar shows "1000+ rows (limit)"). It is configurable up to a hard maximum of **10,000** (`db.MaxRowLimit`); a single query is never unlimited. Paging or streaming more rows comes later.
- Errors show inline in the results pane, with Postgres `position` mapped to an editor line/column marker.
- Every executed query is appended to `~/.local/share/queryme/queries.jsonl` (mode 0600): connection, SQL, timestamp, duration, command tag, row count, error. *shortcut: no rotation yet.* The TUI writes it and `queryme query` does not.
- Tabs share one connection pool. Each tab runs its own query in the background, results go to the tab that ran them even after you switch away, and a running tab shows `●` in the tab bar. Closing a tab cancels its query, and closing the last one leaves a fresh empty tab.

## 6b. Editor: highlighting and autocomplete

- **Highlighting** (`ui.HighlightSQL`): keywords, strings, numbers and `--` / `/* */` comments get coloured in the textarea's rendered view. It only adds escape codes, skipping the gutter and keeping the textarea's own cursor and cursor-line styling, so widths never change, and it adds nothing when colours are off. *shortcut: a string or block comment spanning lines is only coloured on its first line.*
- **Autocomplete** (`complete.Completer`, one shared keyword list with the highlighter): after each change to the editor text, the word before the cursor is completed from the schema, which loads in the background on connect and is shared with the AI (`ai.SchemaCache`; `/refresh` reloads both). Until it arrives, only keywords complete.
  - after `FROM` / `JOIN` / `UPDATE` / `INTO` / `TABLE`: tables and schemas
  - after `alias.` or `table.`: that table's columns, with aliases read from `FROM users u` / `JOIN orders AS o`
  - after `schema.`: that schema's tables
  - elsewhere: columns of the tables the query mentions, then keywords (in the case being typed), then tables, then other columns
- The list pops up under the partial word (above it near the bottom). `Tab` accepts it; `↑↓` or `Ctrl+N/P` move; `Esc` closes. A cursor move without typing, or leaving the editor, also closes it. With no list open, `Tab` moves focus as before. *shortcut: comma-separated FROM lists and subquery aliases aren't resolved, and the popup row is estimated because the textarea hides its scroll offset.*

## 7. Result table rendering

- Column width = min(max(header, widest cell in the visible window), `maxColWidth` = 40, configurable). Longer values get cut with `…` using **display width** (`go-runewidth`), so CJK and emoji don't break alignment.
- Newlines and tabs inside a cell are rendered as `⏎` / `→` in the grid.
- `NULL` renders dim/italic, which keeps it distinct from the string `'NULL'`.
- Horizontal scroll by column when the total width exceeds the terminal. Row numbers stay sticky on the left.
- Mouse: `tea.WithMouseCellMotion()`. A click is hit-tested to (row, col): a left click selects the cell and focuses the results, and a right click also opens the row menu. Clicking the editor focuses it. With mouse capture on, terminal text selection needs `Shift`+drag.
- The grid is custom (`ui.ResultTable`), not `bubbles/table`, which has no horizontal scroll, styled cells or hit-testing. It has a cell cursor (row highlight plus an underlined cell) and the row menu is drawn over it (`ui.Overlay`).

### Clipboard

`clip.Clipboard` pipes to `wl-copy` (Wayland), `xclip` or `xsel` (X11), or `pbcopy` (macOS). If none works, or over SSH (where a local tool would fill the remote clipboard), it writes an **OSC 52** escape sequence, wrapped for tmux when needed. GNOME Terminal/VTE ignores OSC 52, so local Linux use needs `wl-clipboard` or `xclip` installed. The status bar says how it copied, e.g. "Copied row 2 as JSON to the clipboard".

Copy formats: **TSV** (NULL → empty), **JSON** (ordered keys, NULL → `null`, values as strings because the text protocol carries no types), **cell** (raw value, NULL → empty).

## 8. AI design

### Bridge

```
cd ~/.local/share/queryme/claude && claude -p --output-format json \
  --system-prompt "<role + version + schema + rules>" \
  --strict-mcp-config --disable-slash-commands --tools "" \
  [--resume <session_id>]          # follow-ups in the same tab keep context
  < "<request + editor SQL + last error>"
```

- `--tools ""` turns off **every tool**, so Claude can't read files or run commands. `--strict-mcp-config` skips MCP servers and `--disable-slash-commands` skips skills. `--system-prompt` replaces Claude Code's coding prompt with queryme's.
- The request goes on **stdin**. It can be long, and as an argument it would be swallowed by the variadic `--tools` flag.
- claude runs in a **fixed queryme folder**, so `--resume` finds the sessions it saved there and no project `CLAUDE.md` from wherever queryme was started gets loaded.
- The prompt is a 3-line wrapping box under a label line, darkened like the results pane while focused. Enter sends; it never adds a line. While Claude answers, the label reads `✦ Asking Claude` with dots that animate `.` `..` `...` every 350 ms.
- Ctrl+C or closing the tab kills the process; `WaitDelay` stops waiting on pipes a killed process's children may still hold.
- Implementation: `ai.Assistant` (bridge), `ai.SchemaCache` (once per connection, shared by tabs), `ai.RenderSchema`, `ai.SplitReply`. Checked against claude 2.1.309: a first answer took about 8 s, and a resumed follow-up kept the context.
- The JSON reply gives `result` (text) and `session_id`. The Tab stores `session_id`, so "now group it by month" works as a follow-up.
- The SQL is extracted from the first ```sql fenced block. If no block is found, the raw text is shown as an AI note and the editor stays unchanged.
- Where the SQL lands: if the current editor is **empty**, the SQL fills it. If the editor is **non-empty**, the SQL opens in a **new tab** titled `AI: <first words of the request>`, so existing work is never overwritten. The new tab inherits the Claude `session_id`, so follow-ups keep their context. **It never auto-runs.**
- If `claude` is not on PATH or is not logged in, the AI prompt shows a clear message and the rest of the app works normally.

### Context sent to Claude

1. Dialect and server version (`SELECT version()`).
2. Schema summary from `SchemaCache`: `schema.table(col type [pk|fk→t.c], …)`, loaded once per connection and refreshed by typing `/refresh` in the AI prompt.
   *shortcut: when the schema text exceeds about 30k chars, only table names plus the tables matched by keywords in the request get sent. A smarter relevance pass can come when real users hit it.*
3. The current editor buffer, so "fix this" and "optimise this" work.
4. The last error, if the previous run failed, so "fix the error" works.
5. **No row data is ever sent.** Only schema and SQL go to Claude.

### AI actions (v1)

| Prompt style | Behaviour |
|---|---|
| "top 10 customers by revenue last month" | Generate a new query |
| "fix it" (after an error) | Rewrite the buffer using the error message |
| "explain" | Answer appears in a notes area under the prompt; the buffer is unchanged |
| "make it faster" | Rewrite, with optional EXPLAIN advice in notes |

Nothing else for v1: no autonomous agent loop and no auto-run (by design).

## 9. Config

`~/.config/queryme/config.toml`

```toml
[ui]
max_col_width = 40
row_limit = 1000        # 1..10000

[ai]
claude_bin = "claude"   # path override

[[connections]]
name = "prod"
url  = "postgres://app@db.example:5432/app?sslmode=require"
ssh  = "me@bastion.example"   # optional: tunnel through this SSH server
# password: OS keyring (key "queryme/prod"), else ~/.pgpass, else prompt
read_only = true        # session-level `SET default_transaction_read_only = on`: blocks writes from the user too
```

Passwords are never written to the TOML. Typed passwords are kept encrypted in the connection history (§2).

## 10. Roadmap

**M1: Skeleton.** CLI args, config loading, connection picker, pgx connect, single tab, run query, plain result table.
**M2: Result table.** ✅ Truncation with `…`, h-scroll, cell cursor, mouse click, row menu, clipboard with OSC 52, full-row viewer.
**M3: Tabs.** ✅ Multiple tabs, per-tab async queries, cancel, tab switching via keys and mouse, query history file.
**M4: AI.** ✅ `claude -p` bridge, schema cache, prompt line, SQL extraction, per-tab session resume, error-fix flow.
**M5: Polish.** ✅ SQL highlighting in the editor, help overlay, `read_only` profiles, release builds, plus SQL autocomplete.
**M6: Connections.** ✅ Forget recent connections (picker `d`, `connections forget`), SSL docs with the key passphrase kept encrypted and a `tls` header marker, SSH tunnels.

### TODO: more databases (each one implements `db.Driver`)

- [ ] MySQL / MariaDB (`go-sql-driver/mysql`)
- [ ] SQLite (`modernc.org/sqlite`, pure Go)
- [ ] SQL Server (`microsoft/go-mssqldb`)
- [ ] ClickHouse
- [ ] DuckDB
- [ ] MongoDB (non-SQL, which needs a different editor/AI mode, so it's last)

### Later / not in v1

Saving tabs across restarts, a schema browser sidebar, export to CSV, result paging beyond the row cap, editing cells in place.

## 11. Testing

- `postgres.Driver`: integration tests against a throwaway Postgres (`testcontainers-go` or a `docker run` in CI).
- `ResultTable`: golden-file render tests (fixed width, with truncation, wide chars, NULL, and newlines).
- `ai.Assistant`: SQL extraction unit tests, plus a fake `claude` binary on PATH that echoes canned JSON.
- Bubble Tea flows: `teatest` for tab open/close and the click → menu → copy path.

## 12. Resolved decisions

1. AI-generated SQL fills an empty editor. If the editor is non-empty, it opens in a new tab (§8).
2. A `read_only` profile blocks all writes, the user's own included. Postgres enforces this at session level, so there's no app-side SQL parsing.
3. The default row cap is 1,000 rows, configurable with `ui.row_limit` up to a hard maximum of 10,000. A larger value is rejected at startup, and the driver clamps any limit to 10,000.

Status: **design final**. Next step is M1.
