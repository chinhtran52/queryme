package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const killWait = 2 * time.Second

const systemPrompt = `You are the SQL assistant inside queryme, a terminal client for %s.
When the user wants a query, reply with exactly one fenced code block tagged sql holding a single runnable query, then at most two short sentences about it.
When the user asks to explain or advise, answer in plain text and include SQL only if it helps.
Use only the tables and columns in the schema below and quote identifiers that need it. Never claim you ran anything.
If the request mentions the current SQL or an error, they are given after the request.

Schema:
%s`

// Request is one question from a tab.
type Request struct {
	Prompt    string
	Editor    string // current editor SQL, for "fix this" and "optimise this"
	LastError string // last query error, for "fix it"
	SessionID string // continues the tab's Claude conversation when set
}

type Reply struct {
	SQL       string
	Notes     string
	SessionID string
}

// Assistant asks the user's own Claude Code login through `claude -p`, with every tool disabled.
type Assistant struct {
	bin    string
	dir    string
	schema *SchemaCache
}

// NewAssistant runs claude in dir, so --resume finds earlier sessions and no project CLAUDE.md is pulled in.
func NewAssistant(bin, dir string, schema *SchemaCache) *Assistant {
	return &Assistant{bin: bin, dir: dir, schema: schema}
}

type cliResult struct {
	Subtype   string `json:"subtype"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
}

func (a *Assistant) Ask(ctx context.Context, req Request) (Reply, error) {
	schema, err := a.schema.Get(ctx)
	if err != nil {
		return Reply{}, fmt.Errorf("load schema: %w", err)
	}
	if a.dir == "" {
		return Reply{}, errors.New("no data directory to run claude in")
	}
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		return Reply{}, err
	}

	args := []string{
		"-p", "--output-format", "json",
		"--system-prompt", fmt.Sprintf(systemPrompt, schema.Version, RenderSchema(schema, req.Prompt)),
		"--strict-mcp-config", "--disable-slash-commands",
		"--tools", "",
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	cmd := exec.CommandContext(ctx, a.bin, args...)
	cmd.Dir = a.dir
	// After a cancel, stop waiting on output pipes that a killed process's children may still hold.
	cmd.WaitDelay = killWait
	// The prompt goes on stdin: it can be long, and it must not be read as a value of the variadic --tools flag.
	cmd.Stdin = strings.NewReader(userPrompt(req))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		switch {
		case ctx.Err() != nil:
			return Reply{}, ctx.Err()
		case errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist):
			return Reply{}, fmt.Errorf("%q not found: install Claude Code or set ai.claude_bin in the config", a.bin)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return Reply{}, fmt.Errorf("claude failed: %v: %s", err, detail)
	}

	var res cliResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return Reply{}, fmt.Errorf("unexpected claude output: %w", err)
	}
	if res.IsError || res.Subtype != "success" {
		return Reply{}, fmt.Errorf("claude: %s", strings.TrimSpace(res.Result))
	}
	sql, notes := SplitReply(res.Result)
	return Reply{SQL: sql, Notes: notes, SessionID: res.SessionID}, nil
}

func userPrompt(req Request) string {
	var b strings.Builder
	b.WriteString(req.Prompt)
	if strings.TrimSpace(req.Editor) != "" {
		b.WriteString("\n\nCurrent SQL in the editor:\n```sql\n" + req.Editor + "\n```")
	}
	if req.LastError != "" {
		b.WriteString("\n\nThe last run failed with:\n" + req.LastError)
	}
	return b.String()
}

var blankLines = regexp.MustCompile(`\n\s*\n(\s*\n)+`)

var codeBlock = regexp.MustCompile("(?s)```[ \t]*(?:sql|postgresql|postgres|pgsql)?[ \t]*\r?\n(.*?)```")

// SplitReply takes the first fenced code block as SQL and keeps the rest as notes.
func SplitReply(text string) (sql, notes string) {
	m := codeBlock.FindStringSubmatchIndex(text)
	if m == nil {
		return "", strings.TrimSpace(text)
	}
	sql = strings.TrimSpace(text[m[2]:m[3]])
	notes = blankLines.ReplaceAllString(strings.TrimSpace(text[:m[0]]+"\n"+text[m[1]:]), "\n\n")
	return sql, notes
}
