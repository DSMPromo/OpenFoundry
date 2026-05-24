-- Self-service password-reset tokens.
--
-- The flow:
--   1. User hits POST /api/v1/auth/password/reset/request with their
--      email.
--   2. Service generates a 32-byte random token, stores its SHA-256
--      digest here together with the user id and a 1-hour expiry,
--      and sends the plaintext token to the user (via SMTP relay
--      when configured; via the API response in dev-mode).
--   3. User submits the plaintext token plus their new password to
--      POST /api/v1/auth/password/reset/confirm. Service hashes the
--      token, looks up the row, verifies expiry + unused state,
--      stamps used_at in the same transaction as the password
--      update, and rejects subsequent uses.
--
-- Tokens are single-use and short-lived. The plaintext form never
-- touches storage; only its hex-encoded SHA-256 digest is persisted,
-- mirroring the refresh-token pattern already used by this service.
CREATE TABLE IF NOT EXISTS password_reset_tokens (
    token_hash  TEXT PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    requester_ip TEXT
);

CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_user
    ON password_reset_tokens (user_id);
CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_expires
    ON password_reset_tokens (expires_at);
