// Package server wires the chi router for identity-federation-service slice 1.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/signingkeys"
)

// Readiness aggregates boot-time degraded states the operator should
// see on /readyz. Fields are read-only after server construction —
// they reflect decisions taken during main()'s boot sequence (e.g.
// OIDC discovery failed but we kept the process up so the rest of the
// auth surface stays available).
type Readiness struct {
	// OIDCDegraded is true when oidc.NewService returned an error at
	// boot and we fell back to an empty provider set. SSO endpoints
	// will respond with `unknown_provider` until restart.
	OIDCDegraded bool
}

// New builds the http.Server with slice-1 routes.
//
// Slice 1 mounts:
//
//	GET    /healthz
//	GET    /readyz
//	GET    /metrics
//	GET    /api/v1/auth/bootstrap-status
//	POST   /api/v1/auth/register
//	POST   /api/v1/auth/login
//	POST   /api/v1/auth/token/refresh
//
// Subsequent slices add: /auth/sessions/*, /auth/sso/*, /users/*,
// /roles/*, /groups/*, /permissions/*, /policies/*, /control-panel/*,
// /scim/v2/*, /jwks/rotate, /audit/metrics.
func New(cfg *config.Config, jwt *authmw.JWTConfig, auth *handlers.Auth, mfa *handlers.MFA, wa *handlers.WebAuthn, sso *handlers.SSO, ssoAdmin *handlers.SsoAdmin, rbac *handlers.RBAC, jwks *signingkeys.Handler, m *observability.Metrics, ready *Readiness, probes ...capabilities.DependencyProbe) *http.Server {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))
	controlPanel := handlers.NewControlPanel()
	if rbac != nil {
		rbac.ControlPanel = controlPanel
		if auth != nil && auth.Issuer != nil {
			rbac.OAuthIssuer = auth.Issuer
		}
	}
	var scopedSessions *handlers.ScopedSessions
	if auth != nil && auth.Repo != nil && auth.Issuer != nil {
		scopedSessions = handlers.NewScopedSessions(controlPanel, auth.Repo, auth.Issuer)
	}

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		payload := map[string]string{"status": "ready", "oidc": "ok"}
		if ready != nil && ready.OIDCDegraded {
			payload["oidc"] = "degraded"
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(payload)
	})
	r.Method(http.MethodGet, "/metrics", m.Handler())

	// JWKS publication — public, no bearer required (verifiers
	// elsewhere in the platform fetch this to validate RS256 tokens).
	if jwks != nil {
		r.Get("/.well-known/jwks.json", jwks.Jwks)
	}

	// Capability registry — see docs/agent-automation/AGENT-CAPABILITIES-ROADMAP.md (M1.1).
	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	// /api/v1/auth/* — public (no bearer required, the endpoints
	// handle credentials themselves).
	r.Route("/api/v1/auth", func(api chi.Router) {
		api.Get("/bootstrap-status", auth.BootstrapStatus)
		api.Post("/register", auth.Register)
		api.Post("/login", auth.Login)
		api.Post("/logout", auth.Logout)
		api.Post("/token/refresh", auth.Refresh)
		api.Post("/api-key/exchange", auth.ExchangeAPIKey)
		api.Post("/mfa/totp/complete-login", mfa.CompleteLogin)
		api.Post("/mfa/webauthn/login/challenge", wa.LoginChallenge)
		api.Post("/mfa/webauthn/login/finish", wa.LoginFinish)
		api.Get("/sso/providers", sso.ListProviders)
		api.Get("/sso/{provider}/start", sso.Start)
		api.Get("/sso/{provider}/callback", sso.Callback)
		api.Post("/sso/{provider}/acs", sso.AssertionConsumerService)
		// SG.3: unauthenticated login-troubleshooting endpoint.
		// Returns provider responses through IntoResponse() so no
		// secrets leak.
		api.Post("/sso/troubleshoot", ssoAdmin.Troubleshoot)
	})

	// OAuth2 token endpoints for third-party applications. The
	// authorization/consent endpoints are bearer-protected below, but
	// token exchange and revocation authenticate the OAuth client.
	r.Route("/api/v1/oauth2", func(api chi.Router) {
		api.Post("/token", rbac.OAuthToken)
		api.Post("/revoke", rbac.OAuthRevoke)
	})

	// /api/v1/auth/mfa/* — bearer-protected MFA management.
	r.Route("/api/v1/auth/mfa", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))
		api.Get("/status", mfa.Status)
		api.Get("/factors", mfa.ListFactors)
		api.Post("/totp/enroll", mfa.Enroll)
		api.Post("/totp/verify", mfa.Verify)
		api.Post("/totp/disable", mfa.Disable)
		api.Post("/webauthn/register/challenge", wa.RegisterChallenge)
		api.Post("/webauthn/register/finish", wa.RegisterFinish)
	})

	// /api/v1/auth/password — bearer-protected self-service password
	// change. Verifies the current password and replaces the argon2id
	// hash; the session cookie keeps working (no forced re-login).
	r.Route("/api/v1/auth/password", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))
		api.Post("/", auth.ChangePassword)
	})

	// /api/v1/{users,roles,groups,permissions,api-keys} — bearer
	// protected admin surface (slice 6 RBAC CRUD).
	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))

		api.Get("/users", rbac.ListUsers)
		api.Get("/users/me", rbac.Me)
		// SG.4: search/filter envelope alongside the bare-array list.
		api.Get("/users/search", rbac.SearchUsers)
		api.Get("/users/{id}", rbac.GetUser)
		api.Patch("/users/{id}", rbac.UpdateUser)
		api.Delete("/users/{id}", rbac.DeleteUser)
		api.Get("/users/{id}/inspect", rbac.InspectUser)
		api.Post("/users/{id}/restore", rbac.RestoreUser)
		api.Post("/users/{id}/revoke-tokens", rbac.RevokeUserTokens)
		api.Get("/users/{id}/roles", rbac.ListUserRoles)
		api.Put("/users/{id}/roles/{role_id}", rbac.AssignUserRole)
		api.Delete("/users/{id}/roles/{role_id}", rbac.RevokeUserRole)
		// SG.4: admin-only preregistration. RequireAdmin is enforced
		// per-route because the rest of /users (list, get, update) is
		// already gated by the bearer middleware and individual route
		// authorization in the gateway.
		api.With(authmw.RequireAdmin()).Post("/users/preregister", rbac.PreregisterUser)

		api.Get("/roles", rbac.ListRoles)
		api.Post("/roles", rbac.CreateRole)
		api.Get("/roles/{id}", rbac.GetRole)
		api.Patch("/roles/{id}", rbac.UpdateRole)
		api.Delete("/roles/{id}", rbac.DeleteRole)
		api.Put("/roles/{id}/permissions/{permission_id}", rbac.AssignRolePermission)
		api.Delete("/roles/{id}/permissions/{permission_id}", rbac.RevokeRolePermission)

		api.Get("/permissions", rbac.ListPermissions)
		api.Post("/permissions", rbac.CreatePermission)
		api.Delete("/permissions/{id}", rbac.DeletePermission)

		api.Get("/groups", rbac.ListGroups)
		api.Post("/groups", rbac.CreateGroup)
		// SG.5: filter + envelope search alongside the bare-array list.
		api.Get("/groups/search", rbac.SearchGroups)
		api.Get("/groups/{id}", rbac.GetGroup)
		api.Patch("/groups/{id}", rbac.UpdateGroup)
		api.Delete("/groups/{id}", rbac.DeleteGroup)
		api.Get("/groups/{id}/inspect", rbac.InspectGroup)
		api.Get("/groups/{id}/members", rbac.ListGroupMembers)
		api.Put("/groups/{id}/members/{user_id}", rbac.AddGroupMember)
		api.Delete("/groups/{id}/members/{user_id}", rbac.RemoveGroupMember)
		// SG.5: per-group admin grants and nested edges.
		api.Get("/groups/{id}/admins", rbac.ListGroupAdmins)
		api.Post("/groups/{id}/admins", rbac.AddGroupAdmin)
		api.Delete("/groups/{id}/admins/{user_id}", rbac.RemoveGroupAdmin)
		api.Get("/groups/{id}/parents", rbac.ListGroupParents)
		api.Get("/groups/{id}/children", rbac.ListGroupChildren)
		api.Put("/groups/{id}/nested/{member_id}", rbac.AddNestedGroup)
		api.Delete("/groups/{id}/nested/{member_id}", rbac.RemoveNestedGroup)

		api.Get("/api-keys", rbac.ListAPIKeys)
		api.Post("/api-keys", rbac.CreateAPIKey)
		api.Post("/api-keys/leak-scan", rbac.ScanAPIKeyLeaks)
		api.Delete("/api-keys/{id}", rbac.RevokeAPIKey)

		api.Get("/third-party-applications", rbac.ListThirdPartyApplications)
		api.Post("/third-party-applications", rbac.CreateThirdPartyApplication)
		api.Get("/third-party-applications/{id}", rbac.GetThirdPartyApplication)
		api.Patch("/third-party-applications/{id}", rbac.UpdateThirdPartyApplication)
		api.Delete("/third-party-applications/{id}", rbac.DeleteThirdPartyApplication)
		api.Post("/third-party-applications/{id}/rotate-secret", rbac.RotateThirdPartyApplicationSecret)
		api.Put("/third-party-applications/{id}/organizations/{organization_id}/enablement", rbac.UpsertThirdPartyApplicationEnablement)
		api.Delete("/third-party-applications/{id}/organizations/{organization_id}/enablement", rbac.DisableThirdPartyApplicationEnablement)
		api.Get("/third-party-applications/{id}/service-user", rbac.GetThirdPartyApplicationServiceUser)
		api.Post("/third-party-applications/{id}/service-user", rbac.EnsureThirdPartyApplicationServiceUser)
		api.Put("/third-party-applications/{id}/service-user/roles/{role_id}", rbac.AssignThirdPartyServiceUserRole)
		api.Delete("/third-party-applications/{id}/service-user/roles/{role_id}", rbac.RevokeThirdPartyServiceUserRole)
		api.Post("/third-party-applications/{id}/service-user/grants", rbac.CreateThirdPartyServiceUserGrant)
		api.Delete("/third-party-applications/{id}/service-user/grants/{grant_id}", rbac.RevokeThirdPartyServiceUserGrant)
		api.Get("/third-party-applications/{id}/service-user/audit", rbac.ListThirdPartyServiceUserAuditEvents)

		api.Get("/oauth2/authorize", rbac.OAuthAuthorizePrompt)
		api.Post("/oauth2/authorize/consent", rbac.OAuthConsent)
		api.Get("/oauth2/authorizations", rbac.ListOAuthAuthorizations)
		api.Delete("/oauth2/authorizations/{id}", rbac.RevokeOAuthAuthorization)

		api.Get("/control-panel", controlPanel.Get)
		api.Put("/control-panel", controlPanel.Update)
		api.Patch("/control-panel", controlPanel.Update)
		api.Get("/control-panel/upgrade-readiness", controlPanel.UpgradeReadiness)
		api.Post("/control-panel/identity-provider-mappings/preview", controlPanel.PreviewIdentityProviderMapping)
		api.Get("/control-panel/application-access/change-requests", controlPanel.ApplicationAccessChangeRequests)
		api.Post("/control-panel/application-access/change-requests/{id}/decision", controlPanel.DecideApplicationAccessChangeRequest)
		api.Post("/application-access/evaluate", controlPanel.EvaluateApplicationAccess)
		api.Post("/file-access-presets/visible", controlPanel.VisibleFileAccessPresets)
		api.Post("/cross-organization/evaluate", controlPanel.EvaluateCrossOrganization)
		api.Post("/consumer-mode/evaluate", controlPanel.EvaluateConsumerMode)
		api.Post("/control-panel/identity-cache/decision-context", controlPanel.IdentityCacheDecisionContext)
		api.Post("/control-panel/identity-cache/invalidate", controlPanel.InvalidateIdentityCache)

		// Streaming-profile CRUD parked here per ADR-0046. When the
		// streaming WG picks up ADR-0035 P3 these routes will move to
		// /api/v1/streaming-profiles in ingestion-replication-service.
		api.Get("/control-panel/streaming-profiles", controlPanel.ListStreamingProfiles)
		api.Post("/control-panel/streaming-profiles", controlPanel.CreateStreamingProfile)
		api.Get("/control-panel/streaming-profiles/{id}", controlPanel.GetStreamingProfile)
		api.Patch("/control-panel/streaming-profiles/{id}", controlPanel.UpdateStreamingProfile)
		api.Delete("/control-panel/streaming-profiles/{id}", controlPanel.DeleteStreamingProfile)
		api.Post("/control-panel/streaming-profiles/{id}:pause", controlPanel.PauseStreamingProfile)
		api.Post("/control-panel/streaming-profiles/{id}:resume", controlPanel.ResumeStreamingProfile)

		if scopedSessions != nil {
			api.Get("/auth/scoped-sessions", scopedSessions.Options)
			api.Post("/auth/scoped-sessions/select", scopedSessions.Select)
		}

		// /restricted-views — slice 7a (CBAC restricted-view CRUD).
		rv := handlers.NewRestrictedViews(rbac)
		api.Get("/restricted-views", rv.List)
		api.Post("/restricted-views", rv.Create)
		api.Get("/restricted-views/{id}", rv.Get)
		api.Put("/restricted-views/{id}", rv.Update)
		api.Patch("/restricted-views/{id}", rv.Update)
		api.Delete("/restricted-views/{id}", rv.Delete)

		// SG.3: SSO provider admin surface. Bearer-protected + admin-
		// only via authmw.RequireAdmin so only admins can write/read
		// secrets. The boot-time OIDC + SAML registries continue to
		// be seeded from env config; these endpoints are the durable
		// admin source-of-truth a follow-up RFC will hot-load.
		api.Group(func(adm chi.Router) {
			adm.Use(authmw.RequireAdmin())
			adm.Get("/auth/sso/providers", ssoAdmin.List)
			adm.Post("/auth/sso/providers", ssoAdmin.Create)
			adm.Get("/auth/sso/providers/{id}", ssoAdmin.Get)
			adm.Patch("/auth/sso/providers/{id}", ssoAdmin.Update)
			adm.Delete("/auth/sso/providers/{id}", ssoAdmin.Delete)
			adm.Post("/auth/sso/providers/{id}/refresh-metadata", ssoAdmin.RefreshMetadata)
			adm.Get("/auth/sso/providers/{id}/health", ssoAdmin.Health)
			if jwks != nil {
				adm.Post("/admin/jwks/rotate", jwks.Rotate)
			}
		})
	})

	// Synthesise capabilities for every mounted route. OAuth token
	// exchange/revocation stay public at the HTTP layer because they
	// authenticate the OAuth client; authorize/authorization management
	// are listed below as bearer-protected.
	if _, err := caps.IngestChiRoutes(r, capabilities.IngestOptions{
		IDPrefix:  "identity",
		AuthPaths: []string{"/api/v1/auth/mfa", "/api/v1/auth/scoped-sessions", "/api/v1/users", "/api/v1/roles", "/api/v1/groups", "/api/v1/permissions", "/api/v1/api-keys", "/api/v1/third-party-applications", "/api/v1/oauth2/authorize", "/api/v1/oauth2/authorizations", "/api/v1/control-panel", "/api/v1/application-access", "/api/v1/file-access-presets", "/api/v1/cross-organization", "/api/v1/consumer-mode", "/api/v1/restricted-views"},
		Tags:      []string{"identity"},
	}); err != nil {
		panic("identity-federation-service: capability ingest failed: " + err.Error())
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// Run blocks until ctx is done or the listener returns.
func Run(ctx context.Context, srv *http.Server, log *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		log.Info("shutting down")
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
