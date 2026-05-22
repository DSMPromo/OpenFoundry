package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/openfoundry/openfoundry-go/services/report-service/internal/generator"
)

// reportDocument maps a stored execution onto the format-agnostic
// document the generator package renders. It is a pure function of the
// execution, so rendering the artifact again at download time
// reproduces exactly the bytes whose checksum was recorded when the
// report was generated.
func reportDocument(e ReportExecution) generator.Document {
	title := strings.TrimSpace(e.Preview.Headline)
	if title == "" {
		title = e.ReportName
	}
	doc := generator.Document{
		Title:       title,
		Engine:      e.Preview.Engine,
		GeneratedAt: e.GeneratedAt,
	}
	if e.Preview.GeneratedFor != "" {
		doc.Subtitle = "Prepared for " + e.Preview.GeneratedFor
	}
	for _, h := range e.Preview.Highlights {
		doc.Highlights = append(doc.Highlights, generator.Highlight{Label: h.Label, Value: h.Value})
	}
	for _, s := range e.Preview.Sections {
		doc.Sections = append(doc.Sections, generator.Section{
			Title:   s.Title,
			Summary: s.Summary,
			Rows:    s.Rows,
		})
	}
	return doc
}

// Artifact renders and streams the execution's report file in its
// generator format. The bytes are produced on demand from the stored
// execution preview; the generator is deterministic, so they match the
// checksum the execution recorded at generation time.
func (h *ReportsHandler) Artifact(w http.ResponseWriter, r *http.Request) {
	e, err := h.Store.GetExecution(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "report execution not found")
		return
	}
	data, err := generator.Render(string(e.GeneratorKind), reportDocument(*e))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mimeType := e.Artifact.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFileName(e.Artifact.FileName)+`"`)
	w.Header().Set("X-Artifact-Checksum", e.Artifact.Checksum)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// safeFileName drops characters that must not appear unquoted (or at
// all) inside a Content-Disposition filename.
func safeFileName(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == '\\' {
			return -1
		}
		return r
	}, name)
	if cleaned == "" {
		return "report"
	}
	return cleaned
}
