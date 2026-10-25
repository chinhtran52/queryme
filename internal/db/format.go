package db

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Cell returns the raw value; NULL becomes "".
func (r *Result) Cell(row, col int) string {
	if v := r.Rows[row][col]; v != nil {
		return *v
	}
	return ""
}

func (r *Result) RowTSV(row int) string {
	values := make([]string, len(r.Columns))
	for i := range r.Columns {
		values[i] = r.Cell(row, i)
	}
	return strings.Join(values, "\t")
}

// RowJSON keeps the column order. Values stay strings: the text protocol carries no types.
func (r *Result) RowJSON(row int) string {
	var b bytes.Buffer
	r.writeObject(&b, row)
	return indent(b.Bytes())
}

// JSON returns all rows as an array of objects, like RowJSON.
func (r *Result) JSON() string {
	var b bytes.Buffer
	b.WriteByte('[')
	for i := range r.Rows {
		if i > 0 {
			b.WriteByte(',')
		}
		r.writeObject(&b, i)
	}
	b.WriteByte(']')
	return indent(b.Bytes())
}

func indent(compact []byte) string {
	var out bytes.Buffer
	_ = json.Indent(&out, compact, "", "  ") // compact is valid JSON by construction.
	return out.String()
}

func (r *Result) writeObject(b *bytes.Buffer, row int) {
	b.WriteByte('{')
	for i, name := range r.Columns {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(name)
		value := []byte("null")
		if v := r.Rows[row][i]; v != nil {
			value, _ = json.Marshal(*v)
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
}
