// Package handlers wires HTTP endpoints for identity-federation-service slice 1.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/service"
)

// Auth wires register / login / token endpoints.
//
// WebAuthn detection is plugged in via a tiny interface so this file
// stays free of the heavy go-webauthn import surface (the concrete
// implementation lives in internal/webauthn). Pass nil to disable.
type Auth struct {
	Repo     *repo.Repo
	Issuer   *service.Issuer
	WebAuthn WebAuthnChecker // nil → WebAuthn detection skipped

	// SessionCookie controls the of_session cookie emitted alongside
	// the legacy JSON token body on /login, /mfa/totp/complete-login
	// and /token/refresh. The frontend prefers the cookie; the JSON
	// fields stay for one release so non-browser callers keep working.
	SessionCookie SessionCookieConfig
}

// WebAuthnChecker is the bare-minimum surface Auth needs.
type WebAuthnChecker interface {
	HasCredentials(ctx context.Context, userID uuid.UUID) (bool, error)
}

// BootstrapStatus handles GET /api/v1/auth/bootstrap-status.
func (a *Auth) BootstrapStatus(w http.ResponseWriter, r *http.Request) {
	count, err := a.Repo.CountUsers(r.Context())
	if err != nil {
		slog.Error("count users failed", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load bootstrap status")
		return
	}
	writeJSON(w, http.StatusOK, models.BootstrapStatusResponse{
		RequiresInitialAdmin: count == 0,
	})
}

// Register handles POST /api/v1/auth/register.
//
// Mirrors the Rust handler: argon2id password hash, advisory-lock-
// guarded transactional insert, first-user-becomes-admin election.
func (a *Auth) Register(w http.ResponseWriter, r *http.Request) {
	var body models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Email == "" || body.Password == "" || body.Name == "" {
		writeJSONErr(w, http.StatusBadRequest, "email, password and name are required")
		return
	}

	hash, err := service.HashPassword(body.Password)
	if err != nil {
		slog.Error("hash password failed", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	user, role, err := a.Repo.CreateUserAndAssignRole(r.Context(), ids.New(), body.Email, body.Name, hash)
	if err != nil {
		if errors.Is(err, repo.ErrUserExists) {
			writeJSONErr(w, http.StatusConflict, "email already registered")
			return
		}
		slog.Error("register failed", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "registration failed")
		return
	}
	slog.Info("user registered",
		slog.String("user_id", user.ID.String()),
		slog.String("email", user.Email),
		slog.String("role", role),
	)
	writeJSON(w, http.StatusCreated, models.RegisterResponse{
		ID: user.ID, Email: user.Email, Name: user.Name,
	})
}

// Login handles POST /api/v1/auth/login.
//
// Slice 1 scope: password verification + JWT issuance. MFA returns
// `{"status":"mfa_required"}` with the MFA flag set; actual TOTP /
// WebAuthn challenge issuance arrives in slices 3 + 4.
func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var body models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	user, err := a.Repo.FindUserByEmail(r.Context(), body.Email)
	if err != nil {
		slog.Error("lookup user failed", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	if user == nil {
		writeJSONErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !user.IsActive {
		writeJSONErr(w, http.StatusForbidden, "account disabled")
		return
	}

	if err := service.VerifyPassword(body.Password, user.PasswordHash); err != nil {
		writeJSONErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Slice 3: TOTP. WebAuthn arrives in slice 4 — `methods` will
	// grow accordingly. mfa_enforced=true users with no enrolment
	// yet still land in the MFA-required branch; the frontend
	// redirects them through the enrolment flow after the access
	// token has been issued (cannot enrol before authenticating).
	totpCfg, terr := a.Repo.FindTOTPConfig(r.Context(), user.ID)
	if terr != nil {
		slog.Error("login: find totp", slog.String("error", terr.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	totpEnabled := totpCfg != nil && totpCfg.Enabled
	hasWebAuthn := false
	if a.WebAuthn != nil {
		var werr error
		hasWebAuthn, werr = a.WebAuthn.HasCredentials(r.Context(), user.ID)
		if werr != nil {
			slog.Warn("login: webauthn lookup", slog.String("error", werr.Error()))
		}
	}
	if totpEnabled || hasWebAuthn || user.MFAEnforced {
		methods := []string{}
		if totpEnabled {
			methods = append(methods, "totp")
		}
		if hasWebAuthn {
			methods = append(methods, "webauthn")
		}
		challenge, cerr := service.IssueMFAChallenge(a.Issuer.JWT, user, "password")
		if cerr != nil {
			slog.Error("login: mfa challenge", slog.String("error", cerr.Error()))
			writeJSONErr(w, http.StatusInternalServerError, "login failed")
			return
		}
		writeJSON(w, http.StatusOK, models.LoginResponse{
			Status:         models.LoginStatusMFARequired,
			ChallengeToken: challenge,
			Methods:        methods,
			ExpiresIn:      int64(service.ChallengeTTL.Seconds()),
		})
		return
	}

	access, refresh, err := a.Issuer.IssueTokens(r.Context(), user, []string{"password"})
	if err != nil {
		slog.Error("issue tokens failed", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	// SG.4: stamp last_login_at + last_login_ip. Best-effort —
	// failure does not block the authentication response.
	if err := a.Repo.StampLogin(r.Context(), user.ID, time.Now().UTC(), clientIP(r)); err != nil {
		slog.Warn("stamp login", slog.String("user_id", user.ID.String()), slog.String("error", err.Error()))
	}
	SetSessionCookie(w, a.SessionCookie, access, a.Issuer.AccessTTL)
	SetRefreshCookie(w, a.SessionCookie, refresh, a.Issuer.RefreshTTL)
	writeJSON(w, http.StatusOK, models.LoginResponse{
		Status:       models.LoginStatusAuthenticated,
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(a.Issuer.AccessTTL.Seconds()),
	})
}

// changePasswordRequest is the wire shape POSTed to
// /api/v1/auth/password by an authenticated user.
type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// minPasswordLength is the lower bound enforced on new passwords.
// Mirrors the de-facto floor used by the Register flow (8 chars).
const minPasswordLength = 8

// ChangePassword handles POST /api/v1/auth/password. Bearer-protected:
// the caller must have a valid access token attached to a real user
// row in the local realm. The handler verifies the supplied current
// password against the stored argon2id hash, rejects mismatches with
// 401, and on success replaces the hash with a freshly-salted argon2id
// digest of the new password. Returns 204 No Content; the cookie
// session stays valid (no forced re-login).
func (a *Auth) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing session")
		return
	}
	var body changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.CurrentPassword == "" || body.NewPassword == "" {
		writeJSONErr(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}
	if utf8.RuneCountInString(body.NewPassword) < minPasswordLength {
		writeJSONErr(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	if body.CurrentPassword == body.NewPassword {
		writeJSONErr(w, http.StatusBadRequest, "new password must differ from the current one")
		return
	}

	user, err := a.Repo.FindUserByID(r.Context(), claims.Sub)
	if err != nil {
		slog.Error("change password: load user", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "password change failed")
		return
	}
	if user == nil {
		writeJSONErr(w, http.StatusUnauthorized, "user not found")
		return
	}
	if !user.IsActive {
		writeJSONErr(w, http.StatusForbidden, "account disabled")
		return
	}
	if err := service.VerifyPassword(body.CurrentPassword, user.PasswordHash); err != nil {
		writeJSONErr(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	newHash, err := service.HashPassword(body.NewPassword)
	if err != nil {
		slog.Error("change password: hash", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "password change failed")
		return
	}
	if err := a.Repo.UpdatePasswordHash(r.Context(), user.ID, newHash); err != nil {
		slog.Error("change password: persist", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "password change failed")
		return
	}
	slog.Info("password changed",
		slog.String("user_id", user.ID.String()),
		slog.String("email", user.Email),
	)
	w.WriteHeader(http.StatusNoContent)
}

// clientIP picks an honest IP for the request: respects an explicit
// X-Forwarded-For (first hop) when present, otherwise falls back to
// r.RemoteAddr's host part. Empty string is acceptable — the caller
// stores it as NULL.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First hop is the original client.
		if i := strings.Index(xff, ","); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// Refresh handles POST /api/v1/auth/token/refresh.
//
// The refresh token is sourced first from the request body (legacy
// non-cookie consumers) and then from the of_refresh cookie that the
// SPA carries — so the browser flow never exposes a refresh token to
// JavaScript while CLIs / server-to-server callers keep working
// during the deprecation window.
func (a *Auth) Refresh(w http.ResponseWriter, r *http.Request) {
	var body models.RefreshRequest
	if r.Body != nil {
		// An empty body is legitimate (cookie-only callers); ignore
		// decode errors and fall back to the cookie path below.
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	refreshToken := strings.TrimSpace(body.RefreshToken)
	if refreshToken == "" {
		if c, err := r.Cookie(RefreshCookieName); err == nil && c != nil {
			refreshToken = strings.TrimSpace(c.Value)
		}
	}
	if refreshToken == "" {
		writeJSONErr(w, http.StatusBadRequest, "refresh token required")
		return
	}
	access, refresh, err := a.Issuer.RefreshTokens(r.Context(), refreshToken)
	if err != nil {
		// Both Invalid + Reused map to 401 — the client should drop
		// the family and reauthenticate. The slog log keeps them apart.
		slog.Warn("refresh failed", slog.String("error", err.Error()))
		// Reused/invalid refresh: clear the browser cookies so the
		// SPA's next request goes straight to /auth/login instead of
		// spinning on a token the server has already revoked.
		ClearSessionCookie(w, a.SessionCookie)
		writeJSONErr(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	SetSessionCookie(w, a.SessionCookie, access, a.Issuer.AccessTTL)
	SetRefreshCookie(w, a.SessionCookie, refresh, a.Issuer.RefreshTTL)
	writeJSON(w, http.StatusOK, models.TokenResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(a.Issuer.AccessTTL.Seconds()),
	})
}

// Logout handles POST /api/v1/auth/logout.
//
// Invalidates the of_session / of_refresh cookies so the browser
// drops them immediately. The endpoint is intentionally idempotent
// and does not require an existing valid session — calling logout
// twice (or after the cookie already expired) is a 204, not a 401.
//
// Token-level revocation (refresh-token-family invalidation) still
// happens at the issuer; this handler only owns the cookie side of
// logout so the SPA cannot leave a stale Set-Cookie behind.
func (a *Auth) Logout(w http.ResponseWriter, _ *http.Request) {
	ClearSessionCookie(w, a.SessionCookie)
	w.WriteHeader(http.StatusNoContent)
}

// ExchangeAPIKey handles POST /api/v1/auth/api-key/exchange.
//
// Developer API keys are stored as opaque, revocable secrets. This
// endpoint validates expiry/revocation/user-active state, then returns
// a short-lived access JWT that downstream OpenFoundry services can
// verify with the normal bearer-token middleware.
func (a *Auth) ExchangeAPIKey(w http.ResponseWriter, r *http.Request) {
	var body models.ExchangeAPIKeyRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	token := strings.TrimSpace(body.Token)
	if token == "" {
		token = bearerTokenFromRequest(r)
	}
	if token == "" {
		writeJSONErr(w, http.StatusBadRequest, "token required")
		return
	}
	key, user, err := a.Repo.FindUsableAPIKeyByHash(r.Context(), hashAPIKey(token), time.Now().UTC())
	if err != nil {
		slog.Error("api key exchange", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "exchange failed")
		return
	}
	if key == nil || user == nil {
		writeJSONErr(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	access, expiresIn, err := a.Issuer.IssueAccessTokenForAPIKey(user, key)
	if err != nil {
		slog.Error("api key access token", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	writeJSON(w, http.StatusOK, models.APIKeyTokenResponse{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   expiresIn,
		APIKey:      *key,
		Warning:     key.Warning,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
