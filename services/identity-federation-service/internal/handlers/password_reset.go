package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/service"
)

// PasswordResetSMTPConfig wires the optional SMTP relay used to mail
// the reset link to the user. When Host is empty the handler runs
// in "dev mode": the plaintext token is returned in the request
// response so a developer can paste it into the reset URL by hand.
// Production MUST set Host (and From) so tokens never round-trip
// through the API response.
type PasswordResetSMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func (c PasswordResetSMTPConfig) configured() bool {
	return strings.TrimSpace(c.Host) != ""
}

func (c PasswordResetSMTPConfig) addr() string {
	port := c.Port
	if port == 0 {
		port = 587
	}
	return c.Host + ":" + strconv.Itoa(port)
}

func (c PasswordResetSMTPConfig) fromAddress() string {
	if c.From != "" {
		return c.From
	}
	return c.Username
}

// PasswordResetConfig bundles the runtime knobs the reset handlers
// need: where to send the email, what link prefix to embed, and
// whether to expose the dev-mode token in the API response.
type PasswordResetConfig struct {
	SMTP        PasswordResetSMTPConfig
	PublicURL   string // e.g. "https://platform.openfoundry.local"
	DevReturnToken bool  // when true AND SMTP is unconfigured, the
	// request handler echoes the plaintext token in
	// the response. Defaults to true in development.
}

// PasswordReset wires the reset-request and reset-confirm endpoints.
// Both are public (no bearer requirement) — security comes from
// per-token expiry + single-use.
type PasswordReset struct {
	Repo   *repo.Repo
	Config PasswordResetConfig
}

type passwordResetRequest struct {
	Email string `json:"email"`
}

type passwordResetRequestResponse struct {
	// Status is always "ok" — the response intentionally does NOT
	// disclose whether the email matched a real account.
	Status string `json:"status"`
	// DevToken is populated only when SMTP is unconfigured AND
	// DevReturnToken is true; production deployments leave both
	// fields empty.
	DevToken     string `json:"dev_token,omitempty"`
	DevResetURL  string `json:"dev_reset_url,omitempty"`
}

type passwordResetConfirm struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// Request handles POST /api/v1/auth/password/reset/request.
//
// Privacy: the response shape is identical whether the email matches
// a real user or not. An attacker probing for valid emails gets a
// constant 200 back. Operations that depend on email truthiness
// (sending, logging) only happen on the matched path.
func (p *PasswordReset) Request(w http.ResponseWriter, r *http.Request) {
	var body passwordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	email := strings.TrimSpace(strings.ToLower(body.Email))
	if email == "" {
		writeJSONErr(w, http.StatusBadRequest, "email is required")
		return
	}

	user, err := p.Repo.FindUserByEmail(r.Context(), email)
	if err != nil {
		slog.Error("password reset: lookup user", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "password reset failed")
		return
	}

	// Always emit the same response shape — only the side-effects
	// branch on whether the user exists.
	resp := passwordResetRequestResponse{Status: "ok"}

	if user != nil && user.IsActive {
		plaintext, err := service.NewRefreshTokenPlaintext()
		if err != nil {
			slog.Error("password reset: token", slog.String("error", err.Error()))
			writeJSONErr(w, http.StatusInternalServerError, "password reset failed")
			return
		}
		hashed := service.HashRefreshToken(plaintext)
		expires := time.Now().UTC().Add(repo.PasswordResetTokenTTL)
		if err := p.Repo.CreatePasswordResetToken(r.Context(), hashed, user.ID, expires, clientIP(r)); err != nil {
			slog.Error("password reset: persist", slog.String("error", err.Error()))
			writeJSONErr(w, http.StatusInternalServerError, "password reset failed")
			return
		}
		resetURL := buildResetURL(p.Config.PublicURL, plaintext)
		if p.Config.SMTP.configured() {
			if mailErr := sendPasswordResetEmail(p.Config.SMTP, user.Email, resetURL); mailErr != nil {
				slog.Warn("password reset: send email",
					slog.String("user_id", user.ID.String()),
					slog.String("error", mailErr.Error()))
				// We still return ok — the token is persisted and the
				// user can request another mail. Logging the failure
				// is the operator's signal to fix SMTP.
			}
		} else if p.Config.DevReturnToken {
			resp.DevToken = plaintext
			resp.DevResetURL = resetURL
		}
		slog.Info("password reset requested",
			slog.String("user_id", user.ID.String()),
			slog.String("email", user.Email),
			slog.Bool("smtp_configured", p.Config.SMTP.configured()),
		)
	}

	writeJSON(w, http.StatusOK, resp)
}

// Confirm handles POST /api/v1/auth/password/reset/confirm.
//
// Validates the supplied plaintext token (hashes + lookup), rejects
// if expired/used/missing, then atomically rewrites the user's
// password hash and marks the token used in a single transaction.
func (p *PasswordReset) Confirm(w http.ResponseWriter, r *http.Request) {
	var body passwordResetConfirm
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(body.Token) == "" {
		writeJSONErr(w, http.StatusBadRequest, "token is required")
		return
	}
	if utf8.RuneCountInString(body.NewPassword) < 8 {
		writeJSONErr(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}

	hashed := service.HashRefreshToken(body.Token)

	tx, err := p.Repo.BeginTx(r.Context())
	if err != nil {
		slog.Error("password reset: begin tx", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "password reset failed")
		return
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	userID, err := p.Repo.FindAndConsumePasswordResetToken(r.Context(), tx, hashed)
	if err != nil {
		if errors.Is(err, repo.ErrPasswordResetTokenInvalid) {
			writeJSONErr(w, http.StatusBadRequest, "reset token is invalid or expired")
			return
		}
		slog.Error("password reset: consume", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "password reset failed")
		return
	}

	newHash, err := service.HashPassword(body.NewPassword)
	if err != nil {
		slog.Error("password reset: hash", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "password reset failed")
		return
	}
	if err := p.Repo.UpdatePasswordHashTx(r.Context(), tx, userID, newHash); err != nil {
		slog.Error("password reset: persist password", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "password reset failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("password reset: commit", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "password reset failed")
		return
	}
	slog.Info("password reset completed", slog.String("user_id", userID.String()))
	w.WriteHeader(http.StatusNoContent)
}

// buildResetURL renders the URL the user follows from their inbox.
// The frontend mounts /auth/reset-password as the consumer page.
// When PublicURL is empty (local dev) the path is rendered without
// a host prefix so a developer can paste it into their browser at
// http://localhost:55173/auth/reset-password?token=…
func buildResetURL(publicURL, token string) string {
	base := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	return base + "/auth/reset-password?token=" + token
}

// sendPasswordResetEmail composes and ships the reset email through
// the configured SMTP relay. The message is intentionally tiny — a
// subject line and a body that includes the reset URL — so it
// renders identically in every mail client.
func sendPasswordResetEmail(cfg PasswordResetSMTPConfig, to, resetURL string) error {
	from := cfg.fromAddress()
	if from == "" {
		return errors.New("smtp from address is empty")
	}
	subject := "Reset your OpenFoundry password"
	body := "We received a request to reset your OpenFoundry password.\r\n\r\n" +
		"Open the link below to set a new password. The link is valid for one hour and works only once.\r\n\r\n" +
		resetURL + "\r\n\r\n" +
		"If you didn't request this, you can ignore this email — your password stays unchanged."
	msg := []byte(
		"From: " + from + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n" +
			"\r\n" + body,
	)
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	return smtp.SendMail(cfg.addr(), auth, from, []string{to}, msg)
}
