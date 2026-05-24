// Command identity-federation-service slice 1 — register / login / token.
//
// 1:1 with services/identity-federation-service/ (Rust) per the slice
// plan archived at docs/archive/INVENTORY-identity-federation-service.md.
// Subsequent slices layer in sessions, MFA, WebAuthn, SSO, RBAC,
// control panel, hardening + SCIM.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/server"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/service"
	oidcpkg "github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/oidc"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/signingkeys"
	webauthnpkg "github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/webauthn"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.FromEnv()
	if err != nil {
		slog.Error("config load failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if cfg.Service.Version == "dev" {
		cfg.Service.Version = version
	}

	log := observability.InitLogging(cfg.Service.Name, cfg.Service.Version)
	shutdownTracing, err := observability.InitTracing(ctx, cfg.Service.Name, cfg.Service.Version)
	if err != nil {
		log.Error("tracing init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("pgx pool failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()
	if err := repo.Migrate(ctx, pool); err != nil {
		log.Error("migrations failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	jwt := authmw.NewJWTConfig(cfg.JWTSecret).
		WithAccessTTL(cfg.AccessTTL).
		WithRefreshTTL(cfg.RefreshTTL)

	r := &repo.Repo{Pool: pool}
	issuer := &service.Issuer{
		JWT:        jwt,
		Repo:       r,
		AccessTTL:  cfg.AccessTTL,
		RefreshTTL: cfg.RefreshTTL,
	}
	waStore := webauthnpkg.NewPostgresStore(pool)
	waService, err := webauthnpkg.NewService(webauthnpkg.FromEnv(), waStore)
	if err != nil {
		log.Error("webauthn service init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// OIDC providers (slice 5a). BASE_URL is the externally-visible
	// base URL — IdPs MUST register `<BASE_URL>/api/v1/auth/sso/<name>/callback`.
	oidcConfigs, err := oidcpkg.LoadProvidersFromEnv(os.Getenv("BASE_URL"))
	if err != nil {
		log.Error("oidc config failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	metrics := observability.NewMetrics()
	oidcInitFailures := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "identity_oidc_init_failures_total",
		Help: "OIDC provider initialisation failures encountered during service boot. Non-zero means /readyz reports oidc:degraded and SSO endpoints will return unknown_provider until restart.",
	})
	metrics.Register(oidcInitFailures)
	oidcSvc, oidcDegraded, err := initOIDC(ctx, oidcConfigs, oidcInitFailures, log)
	if err != nil {
		log.Error("oidc fallback init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	var mfaSealer *service.Sealer
	if cfg.MFAAtRestKey != "" {
		mfaSealer, err = service.NewSealerFromBase64Key(cfg.MFAAtRestKey)
		if err != nil {
			log.Error("mfa at-rest sealer init failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	} else {
		log.Warn("MFA_AT_REST_KEY unset — TOTP enrolment will return 503 until configured")
	}

	sessionCookie := handlers.SessionCookieConfig{
		Secure:   cfg.CookieSecure,
		SameSite: handlers.ParseSameSite(cfg.CookieSameSite),
		Domain:   cfg.CookieDomain,
	}
	auth := &handlers.Auth{Repo: r, Issuer: issuer, WebAuthn: waService, SessionCookie: sessionCookie}
	mfa := &handlers.MFA{JWT: jwt, Repo: r, Issuer: issuer, Sealer: mfaSealer, SessionCookie: sessionCookie}
	wa := &handlers.WebAuthn{JWT: jwt, Repo: r, Service: waService, Issuer: issuer}
	sso := &handlers.SSO{
		Repo:          r,
		OIDC:          oidcSvc,
		Issuer:        issuer,
		EmitAudit:     handlers.NewOutboxAuditBatcher(r),
		SourceService: cfg.Service.Name,
	}
	ssoAdmin := handlers.NewSsoAdmin(r, nil)
	rbac := &handlers.RBAC{Repo: r}

	// Password reset is wired with optional SMTP. When SMTP_HOST is
	// empty (the default in docker compose), the request handler
	// returns the plaintext token in its response so a dev can paste
	// it into the reset URL. Production deployments MUST set
	// SMTP_HOST so tokens never round-trip through the API.
	smtpPort := 587
	if v := os.Getenv("SMTP_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			smtpPort = n
		}
	}
	passwordReset := &handlers.PasswordReset{
		Repo: r,
		Config: handlers.PasswordResetConfig{
			SMTP: handlers.PasswordResetSMTPConfig{
				Host:     os.Getenv("SMTP_HOST"),
				Port:     smtpPort,
				Username: os.Getenv("SMTP_USERNAME"),
				Password: os.Getenv("SMTP_PASSWORD"),
				From:     firstNonEmpty(os.Getenv("SMTP_FROM"), os.Getenv("SMTP_USERNAME")),
			},
			PublicURL:      os.Getenv("PUBLIC_WEB_ORIGIN"),
			DevReturnToken: !strings.EqualFold(os.Getenv("OPENFOUNDRY_ENV"), "production"),
		},
	}

	// Signing-key rotation (S3.1.c). Manager is wired only when
	// JWT_SIGNING_SEALING_KEY is set — without it the RS256 path
	// stays dormant and the legacy HS256 JWTConfig keeps signing.
	var jwksHandler *signingkeys.Handler
	if sealer, sealErr := signingkeys.NewSealerFromEnv(); sealErr == nil {
		store := signingkeys.NewPostgresStore(pool)
		mgr := signingkeys.NewManager(store, sealer, signingkeys.DefaultPolicy(), nil)
		if err := mgr.EnsureBootstrap(ctx); err != nil {
			log.Error("signing-key bootstrap failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		jwksHandler = signingkeys.NewHandler(mgr)
	} else {
		log.Warn("signing-key rotation disabled — set JWT_SIGNING_SEALING_KEY to enable",
			slog.String("reason", sealErr.Error()))
	}

	srv := server.New(cfg, jwt, auth, mfa, wa, sso, ssoAdmin, rbac, passwordReset, jwksHandler, metrics, &server.Readiness{OIDCDegraded: oidcDegraded}, probes.Postgres("primary", pool))
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// firstNonEmpty returns the first non-empty argument. Used to fall
// back SMTP_FROM to SMTP_USERNAME when only one is set.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// initOIDC builds the OIDC service from `configs`. On discovery failure
// (network unreachable, JWKS missing, …) it logs a warning, increments
// `failures`, and falls back to an empty Service so the rest of the
// auth surface stays available; the returned degraded bool feeds
// /readyz so an operator can spot the masked failure.
//
// The only error path returned to the caller is when the fallback
// itself fails — currently unreachable since oidc.NewService with nil
// configs cannot fail, but kept as a real error so future changes to
// NewService surface here instead of silently corrupting boot.
func initOIDC(ctx context.Context, configs []oidcpkg.ProviderConfig, failures prometheus.Counter, log *slog.Logger) (*oidcpkg.Service, bool, error) {
	svc, err := oidcpkg.NewService(ctx, configs)
	if err == nil {
		return svc, false, nil
	}
	log.Warn("oidc init failed; sso endpoints will return unknown_provider",
		slog.String("error", err.Error()))
	if failures != nil {
		failures.Inc()
	}
	fallback, fbErr := oidcpkg.NewService(ctx, nil)
	if fbErr != nil {
		return nil, true, fbErr
	}
	return fallback, true, nil
}
