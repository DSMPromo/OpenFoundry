package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// fakeTransformRepo is the test double for the new TransformRepository
// surface. It records the most recent inputs so assertions can verify
// the handler routed body fields to the right repo arguments.
type fakeTransformRepo struct {
	gotCreate     models.CreateTransformRequest
	gotOwner      uuid.UUID
	createErr     error
	gotPipelineID uuid.UUID
	gotGraph      string
	savedAt       time.Time
	saveErr       error
}

func (f *fakeTransformRepo) CreateTransform(_ context.Context, req models.CreateTransformRequest, ownerID uuid.UUID) (*models.Transform, error) {
	f.gotCreate = req
	f.gotOwner = ownerID
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &models.Transform{
		ID:        uuid.New(),
		Name:      req.Name,
		Language:  req.Language,
		Source:    req.Source,
		Config:    req.Config,
		OwnerID:   req.OwnerID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}, nil
}

func (f *fakeTransformRepo) SavePipelineBuilderGraph(_ context.Context, pipelineID uuid.UUID, graphJSON string) (*models.PipelineBuilderGraphSave, error) {
	f.gotPipelineID = pipelineID
	f.gotGraph = graphJSON
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	if f.savedAt.IsZero() {
		f.savedAt = time.Now().UTC()
	}
	return &models.PipelineBuilderGraphSave{
		PipelineID:     pipelineID,
		DraftUpdatedAt: f.savedAt,
		GraphJSON:      graphJSON,
	}, nil
}

func postJSON(t *testing.T, handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestValidateTransformAcceptsValidSQL(t *testing.T) {
	rec := postJSON(t, ValidateTransform, map[string]string{
		"language": "SQL",
		"source":   "SELECT id, name FROM users WHERE active = true",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp validateTransformResponse
	mustDecode(t, rec.Body.Bytes(), &resp)
	if !resp.Valid {
		t.Fatalf("valid = false diagnostics=%+v", resp.Diagnostics)
	}
}

func TestValidateTransformFlagsSQLWithoutSelect(t *testing.T) {
	rec := postJSON(t, ValidateTransform, map[string]string{
		"language": "SQL",
		"source":   "UPDATE users SET active = false",
	})
	var resp validateTransformResponse
	mustDecode(t, rec.Body.Bytes(), &resp)
	if resp.Valid {
		t.Fatal("expected invalid for non-SELECT SQL")
	}
	if !containsDiagnostic(resp.Diagnostics, "sql_missing_select") {
		t.Errorf("missing sql_missing_select diagnostic; got %+v", resp.Diagnostics)
	}
}

func TestValidateTransformFlagsUnbalancedParens(t *testing.T) {
	rec := postJSON(t, ValidateTransform, map[string]string{
		"language": "SQL",
		"source":   "SELECT (id + 1 FROM users",
	})
	var resp validateTransformResponse
	mustDecode(t, rec.Body.Bytes(), &resp)
	if !containsDiagnostic(resp.Diagnostics, "sql_unbalanced_parens") {
		t.Errorf("missing sql_unbalanced_parens; got %+v", resp.Diagnostics)
	}
}

func TestValidateTransformFlagsPythonForbiddenImport(t *testing.T) {
	rec := postJSON(t, ValidateTransform, map[string]string{
		"language": "PYTHON",
		"source":   "import os\ndef run(df):\n    return df",
	})
	var resp validateTransformResponse
	mustDecode(t, rec.Body.Bytes(), &resp)
	if resp.Valid {
		t.Fatal("expected invalid when os is imported")
	}
	if !containsDiagnostic(resp.Diagnostics, "python_forbidden_import") {
		t.Errorf("missing python_forbidden_import; got %+v", resp.Diagnostics)
	}
}

func TestValidateTransformWarnsOnPythonWithoutDef(t *testing.T) {
	rec := postJSON(t, ValidateTransform, map[string]string{
		"language": "PYTHON",
		"source":   "x = 1",
	})
	var resp validateTransformResponse
	mustDecode(t, rec.Body.Bytes(), &resp)
	if !resp.Valid {
		t.Fatalf("expected valid (warning only); got diagnostics=%+v", resp.Diagnostics)
	}
	if !containsDiagnostic(resp.Diagnostics, "python_no_top_level_def") {
		t.Errorf("missing python_no_top_level_def; got %+v", resp.Diagnostics)
	}
}

func TestValidateTransformRejectsUnknownLanguage(t *testing.T) {
	rec := postJSON(t, ValidateTransform, map[string]string{
		"language": "RUST",
		"source":   "fn main() {}",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCompileTransformReportsCompiledFlag(t *testing.T) {
	rec := postJSON(t, CompileTransform, map[string]string{
		"language": "SQL",
		"source":   "SELECT 1",
	})
	var resp compileTransformResponse
	mustDecode(t, rec.Body.Bytes(), &resp)
	if !resp.Compiled {
		t.Errorf("compiled = false diagnostics=%+v", resp.Diagnostics)
	}
	if !resp.Deferred {
		t.Errorf("deferred should be true for MVP")
	}
}

func TestPreviewTransformEchoesColumns(t *testing.T) {
	req := previewTransformRequest{
		Language: "SQL",
		Source:   "SELECT id FROM users",
		SampleColumns: []previewSampleColumn{
			{Name: "id", Type: "int64", ValuesJSON: []string{"1", "2", "3"}},
			{Name: "active", Type: "bool", ValuesJSON: []string{"true", "false", "true"}},
		},
		Limit: 10,
	}
	rec := postJSON(t, PreviewTransform, req)
	var resp previewTransformResponse
	mustDecode(t, rec.Body.Bytes(), &resp)
	if !resp.Deferred {
		t.Errorf("deferred should be true for MVP")
	}
	if resp.Previewed {
		t.Errorf("previewed should be false for MVP")
	}
	if resp.RowCount != 3 {
		t.Errorf("row_count = %d, want 3", resp.RowCount)
	}
	if len(resp.Columns) != 2 {
		t.Errorf("columns = %d, want 2", len(resp.Columns))
	}
}

func TestRegisterPythonTransformPersists(t *testing.T) {
	repo := &fakeTransformRepo{}
	restore := SetTransformRepository(repo)
	defer restore()

	owner := uuid.New()
	rec := postJSON(t, RegisterPythonTransform, registerTransformRequest{
		Name:       "filter_active",
		Source:     "def run(df):\n    return df",
		ConfigJSON: `{"timeout_seconds":60}`,
		OwnerID:    &owner,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.gotCreate.Language != models.TransformLanguagePython {
		t.Errorf("language = %q, want PYTHON", repo.gotCreate.Language)
	}
	if repo.gotCreate.Name != "filter_active" {
		t.Errorf("name = %q", repo.gotCreate.Name)
	}
	if repo.gotOwner != owner {
		t.Errorf("owner = %v, want %v", repo.gotOwner, owner)
	}
}

func TestRegisterSqlTransformPersists(t *testing.T) {
	repo := &fakeTransformRepo{}
	restore := SetTransformRepository(repo)
	defer restore()

	rec := postJSON(t, RegisterSqlTransform, registerTransformRequest{
		Name:   "top_users",
		Source: "SELECT * FROM users",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.gotCreate.Language != models.TransformLanguageSQL {
		t.Errorf("language = %q, want SQL", repo.gotCreate.Language)
	}
}

func TestRegisterTransformRejectsInvalidConfig(t *testing.T) {
	repo := &fakeTransformRepo{}
	restore := SetTransformRepository(repo)
	defer restore()

	rec := postJSON(t, RegisterPythonTransform, registerTransformRequest{
		Name:       "x",
		Source:     "def run(): pass",
		ConfigJSON: "not json",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRegisterTransform503WhenRepoMissing(t *testing.T) {
	restore := SetTransformRepository(nil)
	defer restore()

	rec := postJSON(t, RegisterPythonTransform, registerTransformRequest{
		Name:   "x",
		Source: "def run(): pass",
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestRegisterPipelineBuilderGraphSavesGraph(t *testing.T) {
	repo := &fakeTransformRepo{}
	restore := SetTransformRepository(repo)
	defer restore()

	pipelineID := uuid.New()
	graph := `{"nodes":[],"edges":[]}`
	rec := postJSON(t, RegisterPipelineBuilderGraph, models.RegisterPipelineBuilderGraphRequest{
		PipelineID: pipelineID,
		GraphJSON:  graph,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.gotPipelineID != pipelineID {
		t.Errorf("pipeline_id routed wrong: got %v want %v", repo.gotPipelineID, pipelineID)
	}
	if repo.gotGraph != graph {
		t.Errorf("graph_json routed wrong")
	}
}

func TestRegisterPipelineBuilderGraph404WhenMissing(t *testing.T) {
	repo := &fakeTransformRepo{saveErr: models.ErrPipelineNotFound}
	restore := SetTransformRepository(repo)
	defer restore()

	rec := postJSON(t, RegisterPipelineBuilderGraph, models.RegisterPipelineBuilderGraphRequest{
		PipelineID: uuid.New(),
		GraphJSON:  `{}`,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRegisterPipelineBuilderGraphRequiresPipelineID(t *testing.T) {
	repo := &fakeTransformRepo{}
	restore := SetTransformRepository(repo)
	defer restore()

	rec := postJSON(t, RegisterPipelineBuilderGraph, map[string]string{
		"graph_json": "{}",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// --- helpers ---

func mustDecode(t *testing.T, raw []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode response: %v\nbody=%s", err, string(raw))
	}
}

func containsDiagnostic(diagnostics []transformDiagnostic, code string) bool {
	for _, d := range diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}
