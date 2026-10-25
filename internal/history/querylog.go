package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type QueryLogEntry struct {
	Time       time.Time `json:"time"`
	Connection string    `json:"connection"`
	SQL        string    `json:"sql"`
	DurationMS float64   `json:"duration_ms"`
	Command    string    `json:"command,omitempty"`
	Rows       int       `json:"rows,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// QueryLog appends every executed query to a JSON Lines file, readable only by the user.
// shortcut: the file grows forever; add rotation when it gets large in practice.
type QueryLog struct{ path string }

func NewQueryLog(path string) *QueryLog { return &QueryLog{path: path} }

func (l *QueryLog) Append(e QueryLogEntry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
