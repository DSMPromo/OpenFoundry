package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrPipelineNotFound is the sentinel returned when a pipeline-ID
// scoped operation finds no matching row. Both the repo layer and
// the handler package compare against this single value so handler
// translation to HTTP 404 stays decoupled from the postgres import.
var ErrPipelineNotFound = errors.New("pipeline not found")

// TransformLanguage values mirror the proto enum TransformLanguage and
// the `transforms.language` CHECK constraint.
const (
	TransformLanguagePython = "PYTHON"
	TransformLanguageSQL    = "SQL"
)

// NormalizeTransformLanguage upper-cases and trims a caller-supplied
// language string so JSON callers can send "python" / " Python " and
// the wire-level proto enum still rejects malformed values.
func NormalizeTransformLanguage(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)
	s = strings.TrimPrefix(s, "TRANSFORM_LANGUAGE_")
	return s
}

// ValidateTransformLanguage returns an error when language is not one
// of the two accepted variants.
func ValidateTransformLanguage(s string) error {
	switch s {
	case TransformLanguagePython, TransformLanguageSQL:
		return nil
	}
	return fmt.Errorf("invalid transform language %q: must be %q or %q",
		s, TransformLanguagePython, TransformLanguageSQL)
}

// Transform is the persisted `transforms` table row.
type Transform struct {
	ID        uuid.UUID       `json:"id"`
	Name      string          `json:"name"`
	Language  string          `json:"language"`
	Source    string          `json:"source"`
	Config    json.RawMessage `json:"config_json"`
	OwnerID   *uuid.UUID      `json:"owner_id,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// CreateTransformRequest is the wire-level body accepted by both
// RegisterPython and RegisterSql handlers. Language is fixed by the
// caller / handler (not user-supplied) so the same struct serves both.
type CreateTransformRequest struct {
	Name     string          `json:"name"`
	Language string          `json:"-"`
	Source   string          `json:"source"`
	Config   json.RawMessage `json:"config_json,omitempty"`
	OwnerID  *uuid.UUID      `json:"owner_id,omitempty"`
}

// PipelineBuilderGraphSave is the response from
// RegisterPipelineBuilderGraph; mirrors proto PipelineBuilderGraphSaved.
type PipelineBuilderGraphSave struct {
	PipelineID     uuid.UUID `json:"pipeline_id"`
	DraftUpdatedAt time.Time `json:"draft_updated_at"`
	GraphJSON      string    `json:"graph_json"`
}

// RegisterPipelineBuilderGraphRequest is the wire-level body.
type RegisterPipelineBuilderGraphRequest struct {
	PipelineID uuid.UUID `json:"pipeline_id"`
	GraphJSON  string    `json:"graph_json"`
}
