package generator

import (
	"bytes"
	"fmt"
	"strings"
)

// PDF page geometry, in points (1/72").
const (
	pdfPageW   = 612 // US Letter
	pdfPageH   = 792
	pdfMargin  = 54
	pdfContent = pdfPageW - 2*pdfMargin
)

// pdfLine is one laid-out line of text plus the vertical space it
// occupies (leading).
type pdfLine struct {
	text    string
	size    int
	leading int
}

// placedLine is a pdfLine assigned an absolute baseline on a page.
type placedLine struct {
	text     string
	size     int
	baseline int
}

// renderPDF emits a valid PDF 1.7 document with hand-written syntax.
// Helvetica is a standard-14 font so nothing needs to be embedded, and
// the byte layout is fully determined by the input Document.
func renderPDF(doc Document) []byte {
	pages := pdfPaginate(pdfLines(doc))

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")

	// Objects: 1 Catalog, 2 Pages, 3 Font, then per page a Page object
	// (4+2i) and a Contents object (5+2i).
	totalObjs := 3 + 2*len(pages)
	offsets := make([]int, totalObjs+1)
	writeObj := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")

	kids := make([]string, 0, len(pages))
	for i := range pages {
		kids = append(kids, fmt.Sprintf("%d 0 R", 4+2*i))
	}
	writeObj(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>",
		strings.Join(kids, " "), len(pages)))

	writeObj(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica "+
		"/Encoding /WinAnsiEncoding >>")

	for i, page := range pages {
		pageObj, contentObj := 4+2*i, 5+2*i
		writeObj(pageObj, fmt.Sprintf("<< /Type /Page /Parent 2 0 R "+
			"/MediaBox [0 0 %d %d] /Resources << /Font << /F1 3 0 R >> >> "+
			"/Contents %d 0 R >>", pdfPageW, pdfPageH, contentObj))

		stream := pdfContentStream(page)
		offsets[contentObj] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n",
			contentObj, len(stream), stream)
	}

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", totalObjs+1)
	for n := 1; n <= totalObjs; n++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[n])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		totalObjs+1, xrefOffset)
	return buf.Bytes()
}

// pdfLines turns a Document into a flat, wrapped list of lines.
func pdfLines(doc Document) []pdfLine {
	var out []pdfLine
	add := func(text string, size, leading int) {
		for _, w := range pdfWrap(text, size) {
			out = append(out, pdfLine{text: w, size: size, leading: leading})
		}
	}
	title := doc.Title
	if title == "" {
		title = "Report"
	}
	add(title, 22, 32)
	if doc.Subtitle != "" {
		add(doc.Subtitle, 13, 20)
	}
	meta := "Generated " + doc.GeneratedAt
	if doc.Engine != "" {
		meta += "  -  " + doc.Engine
	}
	add(meta, 9, 18)
	for _, h := range doc.Highlights {
		add(h.Label+": "+h.Value, 11, 16)
	}
	for _, s := range doc.Sections {
		out = append(out, pdfLine{size: 8, leading: 12}) // spacer
		add(s.Title, 15, 24)
		if s.Summary != "" {
			add(s.Summary, 10, 16)
		}
		tbl := tabulate(s.Rows)
		if len(tbl.Columns) > 0 {
			add(strings.Join(tbl.Columns, "  |  "), 9, 15)
			for _, row := range tbl.Rows {
				add(strings.Join(row, "  |  "), 9, 13)
			}
		} else {
			add("(no tabular data)", 9, 13)
		}
	}
	return out
}

// pdfWrap word-wraps text to the content width for the given font size.
// Helvetica's average glyph is roughly half the point size wide.
func pdfWrap(text string, size int) []string {
	text = pdfSanitize(text)
	if text == "" {
		return []string{""}
	}
	maxChars := (pdfContent * 2) / size
	if maxChars < 8 {
		maxChars = 8
	}
	var lines []string
	for _, word := range strings.Fields(text) {
		switch {
		case len(lines) == 0:
			lines = []string{word}
		case len(lines[len(lines)-1])+1+len(word) <= maxChars:
			lines[len(lines)-1] += " " + word
		default:
			lines = append(lines, word)
		}
	}
	for len(lines) > 0 && len(lines[len(lines)-1]) > maxChars {
		last := lines[len(lines)-1]
		lines[len(lines)-1] = last[:maxChars]
		lines = append(lines, last[maxChars:])
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// pdfPaginate assigns each line a page and baseline.
func pdfPaginate(lines []pdfLine) [][]placedLine {
	var pages [][]placedLine
	var cur []placedLine
	y := pdfPageH - pdfMargin
	for _, ln := range lines {
		if y-ln.leading < pdfMargin && len(cur) > 0 {
			pages = append(pages, cur)
			cur = nil
			y = pdfPageH - pdfMargin
		}
		y -= ln.leading
		if strings.TrimSpace(ln.text) != "" {
			cur = append(cur, placedLine{text: ln.text, size: ln.size, baseline: y + 3})
		}
	}
	if len(cur) > 0 || len(pages) == 0 {
		pages = append(pages, cur)
	}
	return pages
}

// pdfContentStream renders one page's text-drawing operators.
func pdfContentStream(page []placedLine) string {
	var b strings.Builder
	for _, ln := range page {
		fmt.Fprintf(&b, "BT\n/F1 %d Tf\n%d %d Td\n(%s) Tj\nET\n",
			ln.size, pdfMargin, ln.baseline, pdfEscape(ln.text))
	}
	return b.String()
}

// pdfEscape escapes the three characters that are special inside a PDF
// literal string.
func pdfEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(s)
}

// pdfSanitize reduces text to printable ASCII; Helvetica/WinAnsi covers
// more, but staying in ASCII keeps the byte stream unambiguous.
func pdfSanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 0x20 && r <= 0x7E {
			return r
		}
		if r == '\t' {
			return ' '
		}
		return '?'
	}, s)
}
