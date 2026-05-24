package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/handlers"
	oidcpkg "github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/oidc"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/server"
)

// TestInitOIDC_InvalidConfig_BootsDegraded covers the contract main()
// relies on: when OIDC discovery fails, the process must keep going
// with an empty fallback, the failure must be metered, and /readyz
// must let an operator see the masked state.
func TestInitOIDC_InvalidConfig_BootsDegraded(t *testing.T) {
	t.Parallel()

	// IdP that fails OIDC discovery — go-oidc's NewProvider GETs
	// `<issuer>/.well-known/openid-configuration` and treats any
	// non-2xx (or non-JSON body) as a discovery failure.
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no discovery here", http.StatusNotFound)
	}))
	t.Cleanup(idp.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	failures := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "identity_oidc_init_failures_total_test",
		Help: "test-local counter",
	})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc, degraded, err := initOIDC(ctx, []oidcpkg.ProviderConfig{{
		Name:         "broken",
		IssuerURL:    idp.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		Scopes:       []string{"openid"},
		RedirectURL:  "https://example.invalid/cb",
	}}, failures, log)

	require.NoError(t, err, "fallback init must succeed even when primary OIDC discovery fails")
	require.NotNil(t, svc, "fallback service must be non-nil so handlers can still respond with unknown_provider")
	require.True(t, degraded, "degraded flag must be true after discovery failure")
	require.Empty(t, svc.ProviderNames(), "fallback service must have no providers configured")

	require.Equal(t, float64(1), counterValue(t, failures),
		"identity_oidc_init_failures_total must increment on init failure")

	// /readyz exposes the degraded state so an operator can see it
	// without parsing logs or scraping metrics.
	cfg := &config.Config{}
	cfg.Service.Name = "identity-federation-service"
	srv := server.New(cfg, nil, nil, nil, nil, nil, nil, &handlers.RBAC{}, nil, nil,
		observability.NewMetrics(), &server.Readiness{OIDCDegraded: degraded})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "ready", body["status"])
	require.Equal(t, "degraded", body["oidc"],
		"/readyz must report oidc:degraded when OIDC init failed")
}

// TestInitOIDC_NilConfigs_BootsClean covers the "no OIDC configured"
// path: empty configs must not increment the failure counter nor mark
// the service degraded.
func TestInitOIDC_NilConfigs_BootsClean(t *testing.T) {
	t.Parallel()

	failures := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "identity_oidc_init_failures_total_test_clean",
		Help: "test-local counter",
	})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc, degraded, err := initOIDC(context.Background(), nil, failures, log)

	require.NoError(t, err)
	require.NotNil(t, svc)
	require.False(t, degraded, "no configs is the happy path, not a degraded state")
	require.Zero(t, counterValue(t, failures))

	cfg := &config.Config{}
	cfg.Service.Name = "identity-federation-service"
	srv := server.New(cfg, nil, nil, nil, nil, nil, nil, &handlers.RBAC{}, nil, nil,
		observability.NewMetrics(), &server.Readiness{OIDCDegraded: degraded})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"oidc":"ok"`)
}

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, c.Write(&m))
	return m.GetCounter().GetValue()
}
