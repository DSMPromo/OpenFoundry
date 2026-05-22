package generator

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
)

func sampleDoc() Document {
	return Document{
		Title:       "Quarterly Operations Review",
		Subtitle:    "Q2 — operations team",
		Engine:      "openfoundry-report-local-v1",
		GeneratedAt: "2026-05-22T12:00:00Z",
		Highlights:  []Highlight{{"Datasets", "3"}, {"Sections", "2"}},
		Sections: []Section{
			{Title: "Orders", Summary: "Daily order throughput.", Rows: []map[string]any{
				{"region": "emea", "orders": float64(1200), "growth": "4%"},
				{"region": "amer", "orders": float64(2400), "growth": "7%"},
			}},
			{Title: "Notes", Summary: "Narrative section, no rows."},
		},
	}
}

func TestRenderAllFormatsNonEmptyAndDeterministic(t *testing.T) {
	for _, format := range []string{FormatPDF, FormatExcel, FormatCSV, FormatHTML, FormatPPTX} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			first, err := Render(format, sampleDoc())
			if err != nil {
				t.Fatalf("Render(%s): %v", format, err)
			}
			if len(first) == 0 {
				t.Fatalf("Render(%s): empty output", format)
			}
			second, err := Render(format, sampleDoc())
			if err != nil {
				t.Fatalf("Render(%s) second pass: %v", format, err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("Render(%s) is not deterministic", format)
			}
		})
	}
}

func TestRenderUnsupportedFormat(t *testing.T) {
	if _, err := Render("docx", sampleDoc()); err == nil {
		t.Fatal("expected an error for an unsupported format")
	}
	if Supported("docx") {
		t.Fatal("docx must not report as supported")
	}
}

func TestRenderPDFStructure(t *testing.T) {
	data, err := Render(FormatPDF, sampleDoc())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatal("PDF must start with %PDF-")
	}
	for _, marker := range []string{"startxref", "%%EOF", "/Type /Catalog"} {
		if !bytes.Contains(data, []byte(marker)) {
			t.Fatalf("PDF missing %q", marker)
		}
	}
}

func TestRenderCSVStructure(t *testing.T) {
	data, err := Render(FormatCSV, sampleDoc())
	if err != nil {
		t.Fatal(err)
	}
	rd := csv.NewReader(bytes.NewReader(data))
	rd.FieldsPerRecord = -1
	records, err := rd.ReadAll()
	if err != nil {
		t.Fatalf("CSV did not parse: %v", err)
	}
	if len(records) == 0 || records[0][0] != "Quarterly Operations Review" {
		t.Fatalf("CSV missing title row: %v", records)
	}
	if !bytes.Contains(data, []byte("emea")) {
		t.Fatal("CSV missing section row data")
	}
}

func TestRenderHTMLStructure(t *testing.T) {
	data, err := Render(FormatHTML, sampleDoc())
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"<!DOCTYPE html>", "Quarterly Operations Review", "Orders", "<table>"} {
		if !strings.Contains(s, want) {
			t.Fatalf("HTML missing %q", want)
		}
	}
}

func TestRenderHTMLEscapesReportData(t *testing.T) {
	doc := sampleDoc()
	doc.Sections = []Section{{Title: "Injection", Rows: []map[string]any{
		{"value": "<script>alert(1)</script>"},
	}}}
	data, err := Render(FormatHTML, doc)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("<script>alert(1)</script>")) {
		t.Fatal("HTML must escape report data, not emit raw markup")
	}
}

func TestRenderXLSXIsValidOOXML(t *testing.T) {
	data, err := Render(FormatExcel, sampleDoc())
	if err != nil {
		t.Fatal(err)
	}
	checkOOXML(t, data, "[Content_Types].xml", "xl/workbook.xml", "xl/worksheets/sheet1.xml")
	if !bytes.Contains(data, []byte("PK")) {
		t.Fatal("XLSX must be a zip archive")
	}
}

func TestRenderPPTXIsValidOOXML(t *testing.T) {
	data, err := Render(FormatPPTX, sampleDoc())
	if err != nil {
		t.Fatal(err)
	}
	// One title slide plus one slide per section.
	checkOOXML(t, data, "[Content_Types].xml", "ppt/presentation.xml",
		"ppt/slideMasters/slideMaster1.xml", "ppt/slideLayouts/slideLayout1.xml",
		"ppt/theme/theme1.xml", "ppt/slides/slide1.xml", "ppt/slides/slide3.xml")
}

// checkOOXML asserts data is a zip whose every .xml/.rels part is
// well-formed XML and that each required part is present.
func checkOOXML(t *testing.T, data []byte, required ...string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a valid zip archive: %v", err)
	}
	present := map[string]bool{}
	for _, f := range zr.File {
		present[f.Name] = true
		if !strings.HasSuffix(f.Name, ".xml") && !strings.HasSuffix(f.Name, ".rels") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		dec := xml.NewDecoder(rc)
		for {
			_, err := dec.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				_ = rc.Close()
				t.Fatalf("%s is not well-formed XML: %v", f.Name, err)
			}
		}
		_ = rc.Close()
	}
	for _, name := range required {
		if !present[name] {
			t.Fatalf("OOXML package is missing required part %q", name)
		}
	}
}
