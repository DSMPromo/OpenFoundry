package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/server"
)

func TestRouteSmokeMountsPipelineBuilderRoutes(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "pipeline-build-service"
	cfg.Service.Version = "test"
	cfg.JWTSecret = "test-secret-32bytes-test-secret-3"

	assertRoutesMounted(t, server.BuildRouter(cfg, nil), []routeSmokeCase{
		{http.MethodGet, "/api/v1/pipelines/transforms/catalog"},
		{http.MethodPost, "/api/v1/pipelines/compile-transform"},
		{http.MethodPost, "/api/v1/pipelines/validate-transform"},
		{http.MethodPost, "/api/v1/pipelines/preview-transform"},
		{http.MethodPost, "/api/v1/pipelines/register-python-transform"},
		{http.MethodPost, "/api/v1/pipelines/register-sql-transform"},
		{http.MethodPost, "/api/v1/pipelines/register-pipeline-builder-graph"},
		{http.MethodPost, "/api/v1/pipelines/_validate"},
		{http.MethodPost, "/api/v1/pipelines/_schema-guidance"},
		{http.MethodPost, "/api/v1/pipelines/geospatial/gpx/parse"},
		{http.MethodGet, "/api/v1/pipelines/{id}/nodes/{node_id}/preview"},
		{http.MethodPost, "/api/v1/pipelines/{id}/runs"},
		{http.MethodGet, "/api/v1/data-integration/v1/schedules"},
		{http.MethodPost, "/api/v1/data-integration/v1/schedules"},
		{http.MethodPost, "/api/v1/data-integration/v1/schedules/_scheduler/run-due"},
		{http.MethodPost, "/api/v1/data-integration/v1/schedules/_events"},
		{http.MethodPatch, "/api/v1/data-integration/v1/schedules/{rid}"},
		{http.MethodPost, "/api/v1/data-integration/v1/schedules/{rid}:pause"},
		{http.MethodPost, "/api/v1/data-integration/v1/schedules/{rid}:resume"},
	})
}

type routeSmokeCase struct {
	method string
	path   string
}

func assertRoutesMounted(t *testing.T, handler http.Handler, expected []routeSmokeCase) {
	t.Helper()
	routes, ok := handler.(chi.Routes)
	require.True(t, ok, "handler should expose chi routes")

	seen := map[routeSmokeCase]bool{}
	require.NoError(t, chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		seen[routeSmokeCase{method: method, path: route}] = true
		return nil
	}))

	for _, want := range expected {
		require.True(t, seen[want], "%s %s is not mounted", want.method, want.path)
	}
}

func TestCapabilitiesReportPythonSidecarDependencyUnavailable(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "pipeline-build-service"
	cfg.Service.Version = "test"
	cfg.JWTSecret = "test-secret-32bytes-test-secret-3"

	rr := httptest.NewRecorder()
	server.BuildRouter(cfg, nil).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/_meta/capabilities", nil))

	require.Equal(t, http.StatusOK, rr.Code)
	var payload struct {
		Dependencies []struct {
			Name   string `json:"name"`
			Kind   string `json:"kind"`
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"dependencies"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
	for _, dep := range payload.Dependencies {
		if dep.Name == "python-sidecar" {
			require.Equal(t, "runtime", dep.Kind)
			require.Equal(t, "unavailable", dep.Status)
			require.Contains(t, dep.Error, "PYTHON_SIDECAR_BINARY")
			return
		}
	}
	t.Fatalf("python-sidecar dependency missing from /_meta/capabilities: %+v", payload.Dependencies)
}

func TestCapabilitiesReportPythonSidecarDependencyAvailable(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "pipeline-build-service"
	cfg.Service.Version = "test"
	cfg.JWTSecret = "test-secret-32bytes-test-secret-3"
	cfg.PythonSidecarBinary = "/opt/openfoundry-pyruntime"

	rr := httptest.NewRecorder()
	server.BuildRouter(cfg, nil).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/_meta/capabilities", nil))

	require.Equal(t, http.StatusOK, rr.Code)
	var payload struct {
		Dependencies []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"dependencies"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
	for _, dep := range payload.Dependencies {
		if dep.Name == "python-sidecar" {
			require.Equal(t, "available", dep.Status)
			return
		}
	}
	t.Fatalf("python-sidecar dependency missing from /_meta/capabilities: %+v", payload.Dependencies)
}
