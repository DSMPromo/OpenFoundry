package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/report-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/report-service/internal/handlers"
)

func testToken(t *testing.T, secret string) string {
	t.Helper()
	use := "access"
	tok, err := authmw.EncodeToken(authmw.NewJWTConfig(secret), &authmw.Claims{Sub: uuid.New(), TokenUse: &use, EXP: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestReportRoutesSmokeNo501(t *testing.T) {
	cfg := &config.Config{}
	cfg.Service.Name = "report-service"
	cfg.Service.Version = "test"
	cfg.JWT.Secret = "secret"
	cfg.Server.Addr = "127.0.0.1:0"
	srv, err := New(cfg, observability.NewMetrics(), nil, WithReportStore(handlers.NewMemoryReportStore()))
	if err != nil {
		t.Fatal(err)
	}
	token := testToken(t, cfg.JWT.Secret)
	routes := []struct{ method, path, body string }{
		{"GET", "/api/v1/reports/overview", ""}, {"GET", "/api/v1/reports/catalog", ""}, {"GET", "/api/v1/reports/definitions", ""}, {"GET", "/api/v1/reports/schedules", ""},
	}
	for _, rt := range routes {
		req := httptest.NewRequest(rt.method, rt.path, bytes.NewBufferString(rt.body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		if w.Code == 501 {
			t.Fatalf("%s %s returned 501", rt.method, rt.path)
		}
		if w.Code < 200 || w.Code >= 300 {
			t.Fatalf("%s %s status=%d body=%s", rt.method, rt.path, w.Code, w.Body.String())
		}
	}
}

func TestReportCreateGenerateHistoryDownload(t *testing.T) {
	cfg := &config.Config{}
	cfg.Service.Name = "report-service"
	cfg.Service.Version = "test"
	cfg.JWT.Secret = "secret"
	cfg.Server.Addr = "127.0.0.1:0"
	srv, _ := New(cfg, observability.NewMetrics(), nil, WithReportStore(handlers.NewMemoryReportStore()))
	token := testToken(t, cfg.JWT.Secret)
	body := `{"name":"Ops Daily","owner":"ops","generator_kind":"pdf","dataset_name":"orders","template":{"title":"Ops Daily","sections":[{"id":"s1","title":"Orders","kind":"table","query":"select *","description":"","config":{}}]},"schedule":{"cadence":"daily","timezone":"UTC","anchor_time":"09:00","enabled":true,"next_run_at":"2026-05-19T09:00:00Z"},"recipients":[],"active":true}`
	req := httptest.NewRequest("POST", "/api/v1/reports/definitions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	id := string(bytes.Split(bytes.Split(w.Body.Bytes(), []byte(`"id":"`))[1], []byte(`"`))[0])
	for _, path := range []string{"/api/v1/reports/definitions/" + id + "/generate"} {
		req = httptest.NewRequest("POST", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w = httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		if w.Code == 501 || w.Code >= 300 {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
	req = httptest.NewRequest("GET", "/api/v1/reports/definitions/"+id+"/history", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("history status=%d", w.Code)
	}
}

func TestReportFullFlowCountsAndDownload(t *testing.T) {
	cfg := &config.Config{}
	cfg.Service.Name = "report-service"
	cfg.Service.Version = "test"
	cfg.JWT.Secret = "secret"
	cfg.Server.Addr = "127.0.0.1:0"
	srv, err := New(cfg, observability.NewMetrics(), nil, WithReportStore(handlers.NewMemoryReportStore()))
	if err != nil {
		t.Fatal(err)
	}
	token := testToken(t, cfg.JWT.Secret)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		if w.Code == 501 {
			t.Fatalf("%s %s returned 501", method, path)
		}
		return w
	}

	createBody := `{"name":"Finance Weekly","owner":"finance","generator_kind":"csv","dataset_name":"ledger","template":{"title":"Finance Weekly"},"schedule":{"cadence":"weekly","timezone":"UTC","enabled":true,"next_run_at":"2026-05-19T09:00:00Z"},"recipients":[],"active":true}`
	w := do("POST", "/api/v1/reports/definitions", createBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var def handlers.ReportDefinition
	if err := json.Unmarshal(w.Body.Bytes(), &def); err != nil {
		t.Fatal(err)
	}

	w = do("PATCH", "/api/v1/reports/definitions/"+def.ID, `{"description":"updated"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", w.Code, w.Body.String())
	}

	w = do("POST", "/api/v1/reports/definitions/"+def.ID+"/generate", "")
	if w.Code != http.StatusOK {
		t.Fatalf("generate status=%d body=%s", w.Code, w.Body.String())
	}
	var exec handlers.ReportExecution
	if err := json.Unmarshal(w.Body.Bytes(), &exec); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/v1/reports/definitions", http.StatusOK},
		{"/api/v1/reports/definitions/" + def.ID + "/history", http.StatusOK},
		{"/api/v1/reports/executions/" + exec.ID, http.StatusOK},
		{"/api/v1/reports/executions/" + exec.ID + "/download", http.StatusOK},
	} {
		w = do("GET", tc.path, "")
		if w.Code != tc.want {
			t.Fatalf("%s status=%d body=%s", tc.path, w.Code, w.Body.String())
		}
	}

	w = do("GET", "/api/v1/reports/overview", "")
	if w.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", w.Code, w.Body.String())
	}
	var overview handlers.ReportOverview
	if err := json.Unmarshal(w.Body.Bytes(), &overview); err != nil {
		t.Fatal(err)
	}
	if overview.ReportCount != 1 || overview.ActiveSchedules != 1 || overview.Executions24h != 1 {
		t.Fatalf("unexpected overview: %+v", overview)
	}

	w = do("GET", "/api/v1/reports/schedules", "")
	if w.Code != http.StatusOK {
		t.Fatalf("schedules status=%d body=%s", w.Code, w.Body.String())
	}
	var board handlers.ScheduleBoard
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
		t.Fatal(err)
	}
	if board.ActiveSchedules != 1 || board.PausedReports != 0 {
		t.Fatalf("unexpected schedule board: %+v", board)
	}
}

func TestProductionWithoutDatabaseFailsClosed(t *testing.T) {
	cfg := &config.Config{}
	cfg.Service.Name = "report-service"
	cfg.Service.Version = "test"
	cfg.JWT.Secret = "secret"
	cfg.Server.Addr = "127.0.0.1:0"
	if _, err := New(cfg, observability.NewMetrics(), nil); err == nil {
		t.Fatal("expected server construction to fail without database or explicit memory-store allowance")
	}
}

func TestReportArtifactStreamsRenderedFile(t *testing.T) {
	cfg := &config.Config{}
	cfg.Service.Name = "report-service"
	cfg.Service.Version = "test"
	cfg.JWT.Secret = "secret"
	cfg.Server.Addr = "127.0.0.1:0"
	srv, err := New(cfg, observability.NewMetrics(), nil, WithReportStore(handlers.NewMemoryReportStore()))
	if err != nil {
		t.Fatal(err)
	}
	token := testToken(t, cfg.JWT.Secret)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		return w
	}

	create := `{"name":"Artifact Check","owner":"ops","generator_kind":"pdf","dataset_name":"orders",` +
		`"template":{"title":"Artifact Check","sections":[{"id":"s1","title":"Orders","kind":"table",` +
		`"query":"select 1","description":"","config":{}}]},` +
		`"schedule":{"cadence":"manual","timezone":"UTC","enabled":false},"recipients":[],"active":true}`
	w := do("POST", "/api/v1/reports/definitions", create)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var def handlers.ReportDefinition
	if err := json.Unmarshal(w.Body.Bytes(), &def); err != nil {
		t.Fatal(err)
	}

	w = do("POST", "/api/v1/reports/definitions/"+def.ID+"/generate", "")
	if w.Code != http.StatusOK {
		t.Fatalf("generate status=%d body=%s", w.Code, w.Body.String())
	}
	var exec handlers.ReportExecution
	if err := json.Unmarshal(w.Body.Bytes(), &exec); err != nil {
		t.Fatal(err)
	}
	if exec.Artifact.SizeBytes == 0 || exec.Artifact.Checksum == "" {
		t.Fatalf("generate did not populate real artifact metadata: %+v", exec.Artifact)
	}

	w = do("GET", "/api/v1/reports/executions/"+exec.ID+"/artifact", "")
	if w.Code != http.StatusOK {
		t.Fatalf("artifact status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Fatal("artifact endpoint did not stream a PDF file")
	}
	if int64(len(body)) != exec.Artifact.SizeBytes {
		t.Fatalf("artifact size %d does not match recorded %d", len(body), exec.Artifact.SizeBytes)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("artifact content-type = %q, want application/pdf", ct)
	}
}
