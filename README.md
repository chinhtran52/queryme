# queryme

An AI-first PostgreSQL client for the terminal.

queryme lets you write SQL in tabs, run it, and browse the results without leaving the terminal. Ask Claude to write, fix or explain a query. Claude only puts SQL in the editor and never runs anything: you always press Run yourself.

```
┌ queryme ─ prod (postgres@db.example:5432/app) ─────────────────────────────┐
│ [1 users*] [2 orders] [3 AI: top customers] [+]                            │
├────────────────────────────────────────────────────────────────────────────┤
│  1 SELECT id, email, created_at FROM users                                 │
│  2 WHERE created_at > now() - interval '7 days';                           │
├────────────────────────────────────────────────────────────────────────────┤
│ ✦ Ask AI  ctrl+k · enter to send · esc back                                │
├────────────────────────────────────────────────────────────────────────────┤
│ id │ email                │ created_at                                     │
│  1 │ ann@example.com      │ 2026-10-20 09:12:01                            │
│▶ 2 │ bob.longername@exam… │ 2026-10-21 14:40:55                            │
├────────────────────────────────────────────────────────────────────────────┤
│ 2 rows · 12 ms · row 2/2            ^R run ^K ask ^T tab ^W close ? help   │
└────────────────────────────────────────────────────────────────────────────┘
```

## Features

- **Tabs**: each tab runs its own query in the background, and you can cancel it with `Ctrl+C`.
- **SQL editor** with syntax highlighting and schema-aware autocomplete for tables, columns, aliases and keywords.
- **Result grid**: horizontal scroll, a cell cursor, mouse support, copy a cell or row as TSV/JSON, and a full-row viewer with pretty-printed JSON.
- **AI assistant** that goes through your local [Claude Code](https://claude.com/claude-code) CLI. It needs no API key. Claude sees only the schema and your SQL, never row data, and every tool is turned off for it.
- **Connection picker** listing recent connections, saved profiles and `DATABASE_URL` / `PG*`. Passwords are saved encrypted, with the key kept in the OS keyring.
- **SSH tunnels** that need no local port or `ssh` child process, **SSL/TLS** with the libpq parameters, and **read-only** profiles.
- **Scriptable**: `queryme query` prints results as a table, CSV, TSV or JSON.
- A single static binary for Linux and macOS (amd64 and arm64).

## Install

### One-line install (recommended)

```sh
curl -fsSL https://raw.githubusercontent.com/chinhtran52/queryme/main/install.sh | sh
```

The script finds your OS and CPU, downloads the latest release, checks it against the release checksums, and installs `queryme` to `~/.local/bin`. It doesn't need sudo.

You can change the defaults with environment variables:

```sh
# pin a version and choose the install directory
curl -fsSL https://raw.githubusercontent.com/chinhtran52/queryme/main/install.sh \
  | QUERYME_VERSION=v1.0.1 QUERYME_INSTALL_DIR=/usr/local/bin sh
```

If `~/.local/bin` isn't on your `PATH`, add it:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc   # or ~/.bashrc
```

### With Go

```sh
go install github.com/chinhtran52/queryme/cmd/queryme@latest
```

`queryme version` reports `dev` for binaries built this way.

### Manual download

Download the `queryme_<version>_<os>_<arch>.tar.gz` archive for your platform from [Releases](https://github.com/chinhtran52/queryme/releases), extract it, and put `queryme` somewhere on your `PATH`.

### From source

```sh
git clone https://github.com/chinhtran52/queryme.git
cd queryme
make install            # installs to ~/.local/bin; PREFIX=/usr/local make install to change
```

### Requirements

- Linux or macOS, and a PostgreSQL server to connect to.
- **AI (optional):** the [`claude`](https://claude.com/claude-code) CLI on your `PATH` and logged in. Without it, everything else still works.
- **Linux clipboard:** `wl-clipboard` (Wayland), or `xclip` / `xsel` (X11). Over SSH, queryme falls back to OSC 52.
- **Saved passwords:** an OS keyring, which is the macOS Keychain or GNOME Keyring / KWallet through Secret Service. Without one, queryme asks for the password on every connect.

### Updating

queryme checks GitHub for a newer release in the background each time it runs. If one exists, it prints a notice when it exits. The check never delays startup, and it's skipped for dev builds. Set `QUERYME_NO_UPDATE_CHECK=1` to turn it off.

```sh
queryme update      # download the latest release, verify its checksum, replace the binary
```

If queryme lives in a directory you can't write to (e.g. `/usr/local/bin`), run `sudo queryme update`, or install again with the curl command above.

### Uninstall

```sh
rm ~/.local/bin/queryme
rm -rf ~/.config/queryme ~/.local/share/queryme   # optional: config, history, query log
```

The encryption key stays in the OS keyring under the service `queryme`, user `history-key`. Remove it from there if you want it gone.

## Quick start

```sh
queryme                                         # pick a recent, saved or env connection
queryme postgres://app@localhost:5432/app       # connect to a URL
queryme --name local postgres://app@localhost/app   # ...and remember it by a name
queryme -c prod                                 # use the saved profile "prod"
queryme --ssh me@bastion postgres://app@db.internal/app   # through an SSH tunnel
```

Run SQL without the TUI:

```sh
queryme query -c prod "SELECT count(*) FROM users"
queryme query --url postgres://app@localhost/app -f csv "SELECT * FROM orders" > orders.csv
echo "SELECT now()" | queryme query -f json     # SQL from stdin
```

`query` gets its connection from `--url`, then `-c <profile>`, then `DATABASE_URL` / `PG*`. Formats are `table`, `csv`, `tsv` and `json`. `-l` sets the row limit, up to 10,000.

Manage known connections:

```sh
queryme connections list              # env, saved and recent connections (passwords never shown)
queryme connections rename <url> name # name a recent connection
queryme connections forget <url>      # remove it and its saved password
queryme completion zsh                # shell completion: bash, zsh, fish, powershell
```

## Key bindings

Press `F1` (or `?` in the results) to see every key inside the app.

| Key | Action |
|---|---|
| `Ctrl+R` / `F5` | Run the editor SQL (or the selection) |
| `Ctrl+C` | Cancel a running query or AI request; quit when idle |
| `Ctrl+K` | Ask AI to write, fix or explain SQL (`Enter` sends, `Esc` goes back) |
| `Tab` / `Shift+Tab` | Accept a suggestion, or cycle editor → AI prompt → results |
| `Ctrl+J` | Jump to the SQL editor |
| `Ctrl+T` / `Ctrl+W` | New tab / close tab |
| `Ctrl+Shift+1..9`, `Ctrl+PgUp/PgDn` | Switch tab (or click it; `Alt+1..9` in terminals that send it) |
| `Ctrl+D` / `Ctrl+X` / `Ctrl+V` | Duplicate line / cut line / paste |
| `↑↓←→` / `hjkl`, `g`/`G`, `0`/`$` | Move the cell cursor in results |
| `Enter` / right click | Row menu: copy as TSV or JSON, copy cell, view full row |
| `y` / `Y`, double click | Copy cell / copy row as TSV, copy the clicked cell |
| `Ctrl+Q` | Quit |

Type `/refresh` in the AI prompt to reload the schema after DDL changes. With the mouse on, hold `Shift` and drag to select text in the terminal.

## Configuration

`~/.config/queryme/config.toml` is optional. Use `--config <file>` to point at another file.

```toml
[ui]
max_col_width = 40
row_limit = 1000        # 1..10000

[ai]
claude_bin = "claude"   # path override

[[connections]]
name = "prod"
url  = "postgres://app@db.example:5432/app?sslmode=require"
ssh  = "me@bastion.example"   # optional SSH tunnel
read_only = true              # session-level read-only: blocks every write
```

Passwords never go in the config file. queryme asks for one when it connects and keeps it encrypted in the connection history.

**SSL** is set in the URL: `sslmode` (`disable`, `prefer`, `require`, `verify-ca`, `verify-full`), `sslrootcert`, `sslcert`, `sslkey`, `sslpassword`. File paths must be absolute.

**SSH tunnels** authenticate with `ssh-agent`, then unencrypted `~/.ssh/id_ed25519` / `id_ecdsa` / `id_rsa`. Only host keys already in `~/.ssh/known_hosts` are trusted, so run `ssh` once first. queryme doesn't support `~/.ssh/config` aliases, ProxyJump or password auth.

## Security and privacy

- **AI:** queryme sends Claude the database version, the schema summary, the editor SQL and the last error. **It never sends row data.** Claude runs with every tool, MCP server and slash command turned off. AI-written SQL fills an empty editor or opens a new tab, and nothing runs until you press Run.
- **Passwords** are encrypted with AES-256-GCM in `~/.local/share/queryme/connections.json` (mode 0600). Each one is bound to its URL. The key is kept in the OS keyring.
- **Query log:** every query run in the TUI is appended to `~/.local/share/queryme/queries.jsonl` (mode 0600).

## Development

You need Go (the version is in `go.mod`) and, for the database tests, Docker.

```sh
make build      # bin/queryme with the git version stamped in
make test       # go vet + unit tests
make db-up      # throwaway Postgres 17 on port 55432
make test-db    # tests, including the Postgres-backed ones
make db-down
make snapshot   # local GoReleaser build into dist/
```

The architecture, design decisions and roadmap are in [docs/DESIGN.md](docs/DESIGN.md). Support for MySQL, SQLite and other databases is planned behind the `db.Driver` interface.

**Releasing:** push a `v*` tag (`git tag v1.2.3 && git push origin v1.2.3`). GitHub Actions runs GoReleaser, which publishes the archives and `checksums.txt` that `install.sh` uses.

## License

[MIT](LICENSE)
