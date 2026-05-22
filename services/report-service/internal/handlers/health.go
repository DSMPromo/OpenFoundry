// Package handlers hosts the HTTP handlers for the report-service.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
)

// Health handles GET /healthz. The payload matches the canonical
// OpenFoundry shape so k8s probes and dashboards stay uniform.
func Health(serviceName, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(serviceName, version))
	}
}

// Ready handles GET /readyz. Unlike /healthz (liveness — the process is
// running), readiness fails when the backing store is unreachable, so
// Kubernetes keeps traffic off a pod whose database is down.
func Ready(store ReportStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := store.Ping(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "store unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
