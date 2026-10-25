package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQueryLogAppendsJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "queries.jsonl")
	log := NewQueryLog(path)
	for _, e := range []QueryLogEntry{
		{Time: time.Now(), Connection: "u@h:5432/db", SQL: "SELECT 1", DurationMS: 1.5, Command: "SELECT 1", Rows: 1},
		{Time: time.Now(), Connection: "u@h:5432/db", SQL: "SELECT nope", Error: "boom"},
	} {
		if err := log.Append(e); err != nil {
			t.Fatal(err)
		}
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []QueryLogEntry
	for scanner := bufio.NewScanner(f); scanner.Scan(); {
		var e QueryLogEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		got = append(got, e)
	}
	if len(got) != 2 || got[0].Rows != 1 || got[1].Error != "boom" {
		t.Fatalf("entries = %+v", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}
