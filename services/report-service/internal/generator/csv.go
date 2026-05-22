package generator

import (
	"bytes"
	"encoding/csv"
)

// renderCSV emits the report as a single CSV document. Sections are
// stacked vertically, separated by blank lines; each section prints its
// title, summary and a header + data rows. Records intentionally vary
// in width — csv.Writer does not require a fixed column count.
func renderCSV(doc Document) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	write := func(fields ...string) { _ = w.Write(fields) }

	write(doc.Title)
	if doc.Subtitle != "" {
		write(doc.Subtitle)
	}
	write("Generated", doc.GeneratedAt)
	if doc.Engine != "" {
		write("Engine", doc.Engine)
	}
	for _, h := range doc.Highlights {
		write(h.Label, h.Value)
	}

	for _, s := range doc.Sections {
		write() // blank separator line
		write(s.Title)
		if s.Summary != "" {
			write(s.Summary)
		}
		tbl := tabulate(s.Rows)
		if len(tbl.Columns) > 0 {
			_ = w.Write(tbl.Columns)
			for _, row := range tbl.Rows {
				_ = w.Write(row)
			}
		}
	}

	w.Flush()
	return buf.Bytes()
}
