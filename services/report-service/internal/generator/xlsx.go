package generator

import (
	"bytes"
	"fmt"
)

const xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

// renderXLSX emits a minimal OOXML spreadsheet (.xlsx). Cells use inline
// strings, so there is no shared-string table and no styles part — the
// smallest set of parts that is still a valid workbook.
func renderXLSX(doc Document) ([]byte, error) {
	rows := xlsxRows(doc)

	var sheet bytes.Buffer
	sheet.WriteString(xmlDecl)
	sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for ri, row := range rows {
		fmt.Fprintf(&sheet, `<row r="%d">`, ri+1)
		for ci, val := range row {
			if val == "" {
				continue
			}
			fmt.Fprintf(&sheet,
				`<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`,
				colName(ci), ri+1, xmlEscape(val))
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)

	return buildZip([]zipEntry{
		{"[Content_Types].xml", []byte(xlsxContentTypes)},
		{"_rels/.rels", []byte(xlsxRootRels)},
		{"xl/workbook.xml", []byte(xlsxWorkbook)},
		{"xl/_rels/workbook.xml.rels", []byte(xlsxWorkbookRels)},
		{"xl/worksheets/sheet1.xml", sheet.Bytes()},
	})
}

// xlsxRows lays the document out as one sheet's rows.
func xlsxRows(doc Document) [][]string {
	var rows [][]string
	add := func(cells ...string) { rows = append(rows, cells) }

	title := doc.Title
	if title == "" {
		title = "Report"
	}
	add(title)
	if doc.Subtitle != "" {
		add(doc.Subtitle)
	}
	add("Generated", doc.GeneratedAt)
	if doc.Engine != "" {
		add("Engine", doc.Engine)
	}
	for _, h := range doc.Highlights {
		add(h.Label, h.Value)
	}
	for _, s := range doc.Sections {
		add() // blank spacer row
		add(s.Title)
		if s.Summary != "" {
			add(s.Summary)
		}
		tbl := tabulate(s.Rows)
		if len(tbl.Columns) > 0 {
			add(tbl.Columns...)
			for _, r := range tbl.Rows {
				add(r...)
			}
		}
	}
	return rows
}

const xlsxContentTypes = xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
	`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
	`</Types>`

const xlsxRootRels = xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

const xlsxWorkbook = xmlDecl + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
	`<sheets><sheet name="Report" sheetId="1" r:id="rId1"/></sheets></workbook>`

const xlsxWorkbookRels = xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
	`</Relationships>`
