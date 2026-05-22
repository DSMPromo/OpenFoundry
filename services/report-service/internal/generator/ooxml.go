package generator

import (
	"archive/zip"
	"bytes"
	"strings"
	"time"
)

// zipEpoch is the fixed modification time stamped on every OOXML part,
// so XLSX/PPTX archives are byte-identical across runs.
var zipEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// zipEntry is one part of an OOXML package.
type zipEntry struct {
	name string
	data []byte
}

// buildZip packs entries into a stored (uncompressed) zip with fixed
// timestamps. Stored entries avoid any compressor-dependent variation,
// so the archive is fully deterministic.
func buildZip(entries []zipEntry) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{
			Name:     e.name,
			Method:   zip.Store,
			Modified: zipEpoch,
		})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(e.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// xmlEscape escapes text for inclusion in XML character data or
// attribute values, after dropping characters illegal in XML 1.0.
func xmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	).Replace(sanitizeXML(s))
}

// sanitizeXML removes control characters that XML 1.0 forbids.
func sanitizeXML(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 {
			return r
		}
		return -1
	}, s)
}

// colName converts a zero-based column index to a spreadsheet column
// label (0->A, 25->Z, 26->AA).
func colName(i int) string {
	name := ""
	for i >= 0 {
		name = string(rune('A'+i%26)) + name
		i = i/26 - 1
	}
	return name
}
