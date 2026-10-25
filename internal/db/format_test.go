package db

import "testing"

func text(s string) *string { return &s }

func TestRowFormats(t *testing.T) {
	r := &Result{
		Columns: []string{"id", "note", "gone"},
		Rows:    [][]*string{{text("1"), text("tab\there \"q\""), nil}},
	}
	if got := r.RowTSV(0); got != "1\ttab\there \"q\"\t" {
		t.Errorf("TSV = %q", got)
	}
	want := "{\n  \"id\": \"1\",\n  \"note\": \"tab\\there \\\"q\\\"\",\n  \"gone\": null\n}"
	if got := r.RowJSON(0); got != want {
		t.Errorf("JSON =\n%s\nwant\n%s", got, want)
	}
	if r.Cell(0, 2) != "" || r.Cell(0, 0) != "1" {
		t.Errorf("cells = %q, %q", r.Cell(0, 0), r.Cell(0, 2))
	}
}
