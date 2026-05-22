// Package generator renders report-service executions into downloadable
// artifacts (PDF, Excel, CSV, HTML, PowerPoint).
//
// Every renderer is a pure, deterministic function of its input
// Document: the same Document always produces byte-identical output.
// That lets report-service compute an artifact checksum when a report
// is generated and reproduce the exact same bytes on download without
// persisting the blob.
//
// All five formats use the Go standard library only — no third-party
// dependencies. PDF is emitted as hand-written PDF syntax; XLSX and
// PPTX are emitted as OOXML (zip) packages.
package generator

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Format identifiers accepted by Render. They match the GeneratorKind
// values report-service stores on a ReportDefinition.
const (
	FormatPDF   = "pdf"
	FormatExcel = "excel"
	FormatCSV   = "csv"
	FormatHTML  = "html"
	FormatPPTX  = "pptx"
)

// Document is the format-agnostic input to every renderer.
type Document struct {
	Title       string
	Subtitle    string
	Engine      string
	GeneratedAt string // rendered verbatim; keep stable for deterministic output
	Highlights  []Highlight
	Sections    []Section
}

// Highlight is a headline KPI shown near the top of the report.
type Highlight struct {
	Label string
	Value string
}

// Section is one block of the report: a heading, a one-line summary and
// zero or more data rows.
type Section struct {
	Title   string
	Summary string
	Rows    []map[string]any
}

// Render produces the artifact bytes for format. Unknown formats return
// an error. Output is deterministic for a given Document.
func Render(format string, doc Document) ([]byte, error) {
	switch format {
	case FormatPDF:
		return renderPDF(doc), nil
	case FormatExcel:
		return renderXLSX(doc)
	case FormatCSV:
		return renderCSV(doc), nil
	case FormatHTML:
		return renderHTML(doc)
	case FormatPPTX:
		return renderPPTX(doc)
	default:
		return nil, fmt.Errorf("generator: unsupported format %q", format)
	}
}

// Supported reports whether format has a renderer.
func Supported(format string) bool {
	switch format {
	case FormatPDF, FormatExcel, FormatCSV, FormatHTML, FormatPPTX:
		return true
	default:
		return false
	}
}

// grid is a Section's rows reduced to a fixed, ordered column set.
type grid struct {
	Columns []string
	Rows    [][]string
}

// tabulate flattens []map[string]any into a stable column/row grid. The
// column order is the sorted union of every key present, so the result
// is independent of Go's map iteration order.
func tabulate(rows []map[string]any) grid {
	colSet := map[string]struct{}{}
	for _, r := range rows {
		for k := range r {
			colSet[k] = struct{}{}
		}
	}
	cols := make([]string, 0, len(colSet))
	for k := range colSet {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	out := grid{Columns: cols, Rows: make([][]string, 0, len(rows))}
	for _, r := range rows {
		row := make([]string, len(cols))
		for i, c := range cols {
			row[i] = cell(r[c])
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}

// cell renders an arbitrary JSON-decoded value as one display string.
// Scalars print directly; composite values are JSON-encoded (which
// sorts map keys, keeping the output deterministic).
func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return fmt.Sprintf("%v", t)
	case float64:
		// JSON numbers decode to float64; print integers without a
		// trailing ".0".
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case int, int64:
		return fmt.Sprintf("%d", t)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}
