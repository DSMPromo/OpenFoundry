package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PasswordResetTokenTTL is the standard time-to-live for a freshly
// minted reset token. Long enough for a user to find the email in
// their inbox, short enough that a leaked token is mostly worthless
// the next day.
const PasswordResetTokenTTL = time.Hour

// PasswordResetToken is one row of `password_reset_tokens` projected
// in a Go-native shape. The plaintext token never appears on the
// type — only the SHA-256 digest persisted in the table.
type PasswordResetToken struct {
	TokenHash   string
	UserID      uuid.UUID
	ExpiresAt   time.Time
	UsedAt      *time.Time
	CreatedAt   time.Time
	RequesterIP string
}

// ErrPasswordResetTokenInvalid is returned when the supplied token
// hash either does not match a row, is past its expiry, or has
// already been redeemed. The caller MUST collapse all three cases
// into one HTTP 400 to prevent an attacker probing valid tokens.
var ErrPasswordResetTokenInvalid = errors.New("password reset token is invalid")

// CreatePasswordResetToken stores a freshly minted (hashed) token
// for a user. Expiry is computed by the caller so it can be the
// same value rendered in the email body.
func (r *Repo) CreatePasswordResetToken(ctx context.Context, tokenHash string, userID uuid.UUID, expiresAt time.Time, requesterIP string) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO password_reset_tokens (token_hash, user_id, expires_at, requester_ip)
		 VALUES ($1, $2, $3, NULLIF($4, ''))`,
		tokenHash, userID, expiresAt, requesterIP,
	)
	return err
}

// FindAndConsumePasswordResetToken atomically validates a token and
// marks it used in the same transaction as the password update.
// Returns the bound user id on success, or
// ErrPasswordResetTokenInvalid for any miss (no row / expired /
// already used).
//
// The caller wraps this together with the password_hash UPDATE in a
// single transaction so a process crash between the two writes can
// never leave a row marked used without the password rotated.
func (r *Repo) FindAndConsumePasswordResetToken(ctx context.Context, tx pgx.Tx, tokenHash string) (uuid.UUID, error) {
	var userID uuid.UUID
	var expiresAt time.Time
	var usedAt *time.Time
	err := tx.QueryRow(ctx,
		`SELECT user_id, expires_at, used_at
		 FROM password_reset_tokens
		 WHERE token_hash = $1
		 FOR UPDATE`,
		tokenHash,
	).Scan(&userID, &expiresAt, &usedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrPasswordResetTokenInvalid
	}
	if err != nil {
		return uuid.Nil, err
	}
	if usedAt != nil {
		return uuid.Nil, ErrPasswordResetTokenInvalid
	}
	if time.Now().UTC().After(expiresAt) {
		return uuid.Nil, ErrPasswordResetTokenInvalid
	}
	if _, err := tx.Exec(ctx,
		`UPDATE password_reset_tokens SET used_at = NOW() WHERE token_hash = $1`,
		tokenHash,
	); err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// UpdatePasswordHashTx is the transactional variant of
// UpdatePasswordHash. ConfirmReset binds the password update and the
// reset-token consumption into one transaction so a crash mid-flow
// never strands the user.
func (r *Repo) UpdatePasswordHashTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, newHash string) error {
	tag, err := tx.Exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1`,
		id, newHash,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
