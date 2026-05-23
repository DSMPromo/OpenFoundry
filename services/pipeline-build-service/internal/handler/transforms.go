package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// TransformRepository is the persistence surface for the
// TransformService RPCs: CreateTransform stores a Python/SQL bundle
// in the `transforms` table and SavePipelineBuilderGraph round-trips
// the Pipeline Builder canvas JSON into a pipeline's draft_dag.
type TransformRepository interface {
	CreateTransform(ctx context.Context, req models.CreateTransformRequest, ownerID uuid.UUID) (*models.Transform, error)
	SavePipelineBuilderGraph(ctx context.Context, pipelineID uuid.UUID, graphJSON string) (*models.PipelineBuilderGraphSave, error)
}

type transformRepoSlot struct {
	repo TransformRepository
}

var transformRepoValue atomic.Value // stores *transformRepoSlot

// SetTransformRepository injects the production transforms repo. Tests
// can swap a fake and call the returned restore function on cleanup.
func SetTransformRepository(repo TransformRepository) func() {
	previous, _ := transformRepoValue.Load().(*transformRepoSlot)
	transformRepoValue.Store(&transformRepoSlot{repo: repo})
	return func() { transformRepoValue.Store(previous) }
}

func currentTransformRepository() (TransformRepository, bool) {
	slot, _ := transformRepoValue.Load().(*transformRepoSlot)
	if slot == nil || slot.repo == nil {
		return nil, false
	}
	return slot.repo, true
}

func requireTransformRepository(w http.ResponseWriter, detail string) (TransformRepository, bool) {
	repo, ok := currentTransformRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":  "transform_repository_not_configured",
			"detail": detail,
		})
		return nil, false
	}
	return repo, true
}

// transformDiagnostic mirrors proto ValidationDiagnostic on the wire.
type transformDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Line     uint32 `json:"line"`
	Column   uint32 `json:"column"`
}

const (
	diagSeverityError   = "ERROR"
	diagSeverityWarning = "WARNING"
)

// compileTransformRequest / validateTransformRequest share the same
// payload but go through separate routes; the response shapes differ.
type compileValidateRequest struct {
	Language   string `json:"language"`
	Source     string `json:"source"`
	ConfigJSON string `json:"config_json,omitempty"`
}

type validateTransformResponse struct {
	Valid       bool                  `json:"valid"`
	Diagnostics []transformDiagnostic `json:"diagnostics"`
}

type compileTransformResponse struct {
	Compiled     bool                  `json:"compiled"`
	Deferred     bool                  `json:"deferred"`
	Diagnostics  []transformDiagnostic `json:"diagnostics"`
	CompiledForm string                `json:"compiled_form,omitempty"`
}

// CompileTransform performs structural / lint-level compile of the
// caller's source. A real Python AST compiler and a SQL planner are
// tracked as follow-ups; until those land the response carries
// `deferred=true` so SDK consumers know the contract is honored but
// the runtime is still maturing.
func CompileTransform(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeCompileValidate(w, r)
	if !ok {
		return
	}
	diagnostics := lintTransform(req.Language, req.Source)
	resp := compileTransformResponse{
		Compiled:    !hasErrorDiagnostic(diagnostics),
		Deferred:    true,
		Diagnostics: diagnostics,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ValidateTransform shares the linter with CompileTransform but
// returns the lighter validate response. Cheap to call from a draft
// editor on every keystroke.
func ValidateTransform(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeCompileValidate(w, r)
	if !ok {
		return
	}
	diagnostics := lintTransform(req.Language, req.Source)
	writeJSON(w, http.StatusOK, validateTransformResponse{
		Valid:       !hasErrorDiagnostic(diagnostics),
		Diagnostics: diagnostics,
	})
}

func decodeCompileValidate(w http.ResponseWriter, r *http.Request) (compileValidateRequest, bool) {
	var req compileValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "detail": err.Error()})
		return req, false
	}
	req.Language = models.NormalizeTransformLanguage(req.Language)
	if err := models.ValidateTransformLanguage(req.Language); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_language", "detail": err.Error()})
		return req, false
	}
	return req, true
}

// pythonForbiddenImportRe and sqlMissingFromRe are the two cheap
// structural checks the MVP linter runs. They are intentionally
// conservative — better to defer than to false-positive on real code.
var (
	pythonForbiddenImportRe = regexp.MustCompile(`(?m)^\s*(?:import|from)\s+(os|sys|subprocess|socket)\b`)
	sqlSelectRe             = regexp.MustCompile(`(?is)^\s*SELECT\b`)
)

// lintTransform runs the MVP language-specific checks and returns the
// diagnostics list (empty when the source passes).
func lintTransform(language, source string) []transformDiagnostic {
	out := []transformDiagnostic{}
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return append(out, transformDiagnostic{
			Severity: diagSeverityError,
			Code:     "source_empty",
			Message:  "transform source must not be empty",
		})
	}
	switch language {
	case models.TransformLanguagePython:
		if !strings.Contains(trimmed, "def ") && !strings.Contains(trimmed, "class ") {
			out = append(out, transformDiagnostic{
				Severity: diagSeverityWarning,
				Code:     "python_no_top_level_def",
				Message:  "no top-level def/class found — transform body may not be discoverable",
			})
		}
		if match := pythonForbiddenImportRe.FindStringSubmatch(trimmed); len(match) > 1 {
			out = append(out, transformDiagnostic{
				Severity: diagSeverityError,
				Code:     "python_forbidden_import",
				Message:  "import of " + match[1] + " is not allowed in pipeline transforms",
			})
		}
	case models.TransformLanguageSQL:
		if !sqlSelectRe.MatchString(trimmed) {
			out = append(out, transformDiagnostic{
				Severity: diagSeverityError,
				Code:     "sql_missing_select",
				Message:  "SQL transform must start with a SELECT statement",
			})
		}
		if openP, closeP := strings.Count(trimmed, "("), strings.Count(trimmed, ")"); openP != closeP {
			out = append(out, transformDiagnostic{
				Severity: diagSeverityError,
				Code:     "sql_unbalanced_parens",
				Message:  "SQL has unbalanced parentheses",
			})
		}
	}
	return out
}

func hasErrorDiagnostic(diagnostics []transformDiagnostic) bool {
	for _, d := range diagnostics {
		if d.Severity == diagSeverityError {
			return true
		}
	}
	return false
}

// previewSampleColumn / previewColumn share a wire shape with the
// proto messages (PreviewSampleColumn, PreviewColumn).
type previewSampleColumn struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	ValuesJSON []string `json:"values_json"`
}

type previewTransformRequest struct {
	Language      string                `json:"language"`
	Source        string                `json:"source"`
	ConfigJSON    string                `json:"config_json,omitempty"`
	SampleColumns []previewSampleColumn `json:"sample_columns"`
	Limit         uint32                `json:"limit"`
}

type previewColumn struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	ValuesJSON []string `json:"values_json"`
}

type previewTransformResponse struct {
	Previewed   bool                  `json:"previewed"`
	Deferred    bool                  `json:"deferred"`
	Columns     []previewColumn       `json:"columns"`
	RowCount    uint32                `json:"row_count"`
	Diagnostics []transformDiagnostic `json:"diagnostics"`
}

// PreviewTransform returns a deferred preview today. The lightweight
// runtime is wired only to persisted pipeline nodes; previewing a
// standalone transform from inline samples lands in a follow-up. The
// response still echoes the input columns so SDK consumers can build
// their UI against the final shape.
func PreviewTransform(w http.ResponseWriter, r *http.Request) {
	var req previewTransformRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "detail": err.Error()})
		return
	}
	req.Language = models.NormalizeTransformLanguage(req.Language)
	if err := models.ValidateTransformLanguage(req.Language); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_language", "detail": err.Error()})
		return
	}
	diagnostics := lintTransform(req.Language, req.Source)
	cols := make([]previewColumn, 0, len(req.SampleColumns))
	rowCount := uint32(0)
	for _, c := range req.SampleColumns {
		cols = append(cols, previewColumn(c))
		if uint32(len(c.ValuesJSON)) > rowCount {
			rowCount = uint32(len(c.ValuesJSON))
		}
	}
	writeJSON(w, http.StatusOK, previewTransformResponse{
		Previewed:   false,
		Deferred:    true,
		Columns:     cols,
		RowCount:    rowCount,
		Diagnostics: diagnostics,
	})
}

// registerTransformRequest is shared by the Python and SQL register
// handlers; the language is fixed by the route, not the body.
type registerTransformRequest struct {
	Name       string     `json:"name"`
	Source     string     `json:"source"`
	ConfigJSON string     `json:"config_json,omitempty"`
	OwnerID    *uuid.UUID `json:"owner_id,omitempty"`
}

// RegisterPythonTransform stores a Python transform definition.
func RegisterPythonTransform(w http.ResponseWriter, r *http.Request) {
	registerTransform(w, r, models.TransformLanguagePython)
}

// RegisterSqlTransform stores a SQL transform definition.
func RegisterSqlTransform(w http.ResponseWriter, r *http.Request) {
	registerTransform(w, r, models.TransformLanguageSQL)
}

func registerTransform(w http.ResponseWriter, r *http.Request, language string) {
	repo, ok := requireTransformRepository(w, "transform registration requires DATABASE_URL-backed repository wiring")
	if !ok {
		return
	}
	var body registerTransformRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "detail": err.Error()})
		return
	}
	createReq := models.CreateTransformRequest{
		Name:     body.Name,
		Language: language,
		Source:   body.Source,
		OwnerID:  body.OwnerID,
	}
	if body.ConfigJSON != "" {
		createReq.Config = json.RawMessage(body.ConfigJSON)
		if !json.Valid(createReq.Config) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_config", "detail": "config_json is not valid JSON"})
			return
		}
	}
	ownerID := uuid.Nil
	if body.OwnerID != nil {
		ownerID = *body.OwnerID
	}
	t, err := repo.CreateTransform(r.Context(), createReq, ownerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "create_transform_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// RegisterPipelineBuilderGraph saves the canvas graph JSON into the
// named pipeline's draft_dag column. The pipeline must exist; we
// return 404 with a structured error otherwise.
func RegisterPipelineBuilderGraph(w http.ResponseWriter, r *http.Request) {
	repo, ok := requireTransformRepository(w, "graph save requires DATABASE_URL-backed repository wiring")
	if !ok {
		return
	}
	var body models.RegisterPipelineBuilderGraphRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "detail": err.Error()})
		return
	}
	if body.PipelineID == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "detail": "pipeline_id is required"})
		return
	}
	saved, err := repo.SavePipelineBuilderGraph(r.Context(), body.PipelineID, body.GraphJSON)
	if err != nil {
		if errors.Is(err, models.ErrPipelineNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline_not_found", "detail": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "save_graph_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, saved)
}
