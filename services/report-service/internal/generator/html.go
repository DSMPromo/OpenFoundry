package generator

import (
	"bytes"
	"html/template"
)

// htmlData is the flattened view passed to the HTML template: Section
// rows are pre-tabulated into columns/rows so the template stays simple.
type htmlData struct {
	Title       string
	Subtitle    string
	Engine      string
	GeneratedAt string
	Highlights  []Highlight
	Sections    []htmlSection
}

type htmlSection struct {
	Title   string
	Summary string
	Columns []string
	Rows    [][]string
}

var htmlTemplate = template.Must(template.New("report").Parse(htmlSource))

// renderHTML emits a standalone, styled HTML document. html/template
// escapes every interpolated value, so arbitrary report data cannot
// inject markup.
func renderHTML(doc Document) ([]byte, error) {
	data := htmlData{
		Title:       doc.Title,
		Subtitle:    doc.Subtitle,
		Engine:      doc.Engine,
		GeneratedAt: doc.GeneratedAt,
		Highlights:  doc.Highlights,
	}
	for _, s := range doc.Sections {
		tbl := tabulate(s.Rows)
		data.Sections = append(data.Sections, htmlSection{
			Title:   s.Title,
			Summary: s.Summary,
			Columns: tbl.Columns,
			Rows:    tbl.Rows,
		})
	}
	var buf bytes.Buffer
	if err := htmlTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const htmlSource = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
:root { color-scheme: light dark; }
body { font: 15px/1.55 -apple-system, Segoe UI, Roboto, sans-serif; margin: 0; padding: 2.5rem; color: #1a1d24; background: #f6f7f9; }
main { max-width: 880px; margin: 0 auto; background: #fff; border: 1px solid #e3e6eb; border-radius: 12px; padding: 2.25rem 2.5rem; }
h1 { font-size: 1.7rem; margin: 0 0 .25rem; }
.subtitle { font-size: 1.05rem; color: #555b66; margin: 0 0 .5rem; }
.meta { font-size: .82rem; color: #8a909c; margin: 0 0 1.5rem; }
.highlights { display: flex; flex-wrap: wrap; gap: .75rem; margin: 0 0 1.75rem; }
.highlight { background: #f0f3f8; border: 1px solid #e3e6eb; border-radius: 8px; padding: .6rem .9rem; }
.highlight .label { font-size: .72rem; text-transform: uppercase; letter-spacing: .04em; color: #8a909c; }
.highlight .value { font-size: 1.15rem; font-weight: 600; }
section { border-top: 1px solid #eceef2; padding-top: 1.25rem; margin-top: 1.25rem; }
section h2 { font-size: 1.15rem; margin: 0 0 .35rem; }
section p { color: #555b66; margin: 0 0 .75rem; }
table { border-collapse: collapse; width: 100%; font-size: .9rem; }
th, td { text-align: left; padding: .45rem .6rem; border-bottom: 1px solid #eceef2; }
th { background: #f6f7f9; font-weight: 600; }
.empty { color: #8a909c; font-style: italic; }
</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
{{if .Subtitle}}<p class="subtitle">{{.Subtitle}}</p>{{end}}
<p class="meta">Generated {{.GeneratedAt}}{{if .Engine}} &middot; {{.Engine}}{{end}}</p>
{{if .Highlights}}<div class="highlights">
{{range .Highlights}}<div class="highlight"><div class="label">{{.Label}}</div><div class="value">{{.Value}}</div></div>{{end}}
</div>{{end}}
{{range .Sections}}<section>
<h2>{{.Title}}</h2>
{{if .Summary}}<p>{{.Summary}}</p>{{end}}
{{if .Columns}}<table>
<thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}</tr></thead>
<tbody>{{range .Rows}}<tr>{{range .}}<td>{{.}}</td>{{end}}</tr>{{end}}</tbody>
</table>{{else}}<p class="empty">No tabular data for this section.</p>{{end}}
</section>{{end}}
</main>
</body>
</html>
`
