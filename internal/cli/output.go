package cli

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/chinhtran52/queryme/internal/db"
	"github.com/chinhtran52/queryme/internal/ui"
)

type resultWriter func(io.Writer, *db.Result) error

var formats = map[string]resultWriter{
	"table": writeTable,
	"csv":   writeCSV,
	"tsv":   writeTSV,
	"json":  writeJSON,
}

func formatNames() []string {
	names := make([]string, 0, len(formats))
	for name := range formats {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// writeTable prints a psql-style aligned table. Values are never cut; newlines and tabs show as ⏎ and →.
func writeTable(w io.Writer, res *db.Result) error {
	widths := make([]int, len(res.Columns))
	for i, name := range res.Columns {
		widths[i] = runewidth.StringWidth(name)
		for _, row := range res.Rows {
			widths[i] = max(widths[i], runewidth.StringWidth(ui.CellText(row[i])))
		}
	}
	line := func(cells []string) string {
		padded := make([]string, len(cells))
		for i, cell := range cells {
			padded[i] = " " + cell + strings.Repeat(" ", widths[i]-runewidth.StringWidth(cell)) + " "
		}
		return strings.TrimRight(strings.Join(padded, "|"), " ")
	}
	rules := make([]string, len(widths))
	for i, width := range widths {
		rules[i] = strings.Repeat("-", width+2)
	}

	var b strings.Builder
	b.WriteString(line(res.Columns) + "\n" + strings.Join(rules, "+") + "\n")
	for _, row := range res.Rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = ui.CellText(cell)
		}
		b.WriteString(line(cells) + "\n")
	}
	noun := "rows"
	if len(res.Rows) == 1 {
		noun = "row"
	}
	fmt.Fprintf(&b, "(%d %s)\n", len(res.Rows), noun)
	_, err := io.WriteString(w, b.String())
	return err
}

// writeCSV writes NULL as an empty field.
func writeCSV(w io.Writer, res *db.Result) error {
	out := csv.NewWriter(w)
	if err := out.Write(res.Columns); err != nil {
		return err
	}
	for i := range res.Rows {
		record := make([]string, len(res.Columns))
		for j := range res.Columns {
			record[j] = res.Cell(i, j)
		}
		if err := out.Write(record); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

func writeTSV(w io.Writer, res *db.Result) error {
	var b strings.Builder
	b.WriteString(strings.Join(res.Columns, "\t") + "\n")
	for i := range res.Rows {
		b.WriteString(res.RowTSV(i) + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeJSON(w io.Writer, res *db.Result) error {
	_, err := fmt.Fprintln(w, res.JSON())
	return err
}
