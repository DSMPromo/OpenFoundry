package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// ─── Users ──────────────────────────────────────────────────────────────

// ListUsers returns the most recent 200 non-deleted users. SG.4
// callers prefer ListUsersFiltered for search + pagination.
func (r *Repo) ListUsers(ctx context.Context) ([]models.User, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+userSelectColumns+`
		 FROM users WHERE deleted_at IS NULL
		 ORDER BY created_at DESC LIMIT 200`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUserRows(rows)
}

// ListUsersFiltered returns the users matching `f`. Supports
// case-insensitive substring search on email + username, exact filter
// on organization_id and realm, and active/inactive/deleted status.
// SG.4: drives the admin users UI.
func (r *Repo) ListUsersFiltered(ctx context.Context, f *models.ListUsersFilter) ([]models.User, int64, error) {
	if f == nil {
		f = &models.ListUsersFilter{}
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	args := make([]any, 0, 8)
	conds := make([]string, 0, 6)
	if !f.IncludeDeleted {
		conds = append(conds, "deleted_at IS NULL")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		conds = append(conds,
			fmt.Sprintf("(LOWER(email) LIKE $%d OR LOWER(COALESCE(username,'')) LIKE $%d OR LOWER(name) LIKE $%d)",
				len(args), len(args), len(args)))
	}
	if f.OrganizationID != nil {
		args = append(args, *f.OrganizationID)
		conds = append(conds, fmt.Sprintf("organization_id = $%d", len(args)))
	}
	if realm := strings.TrimSpace(f.Realm); realm != "" {
		args = append(args, realm)
		conds = append(conds, fmt.Sprintf("realm = $%d", len(args)))
	}
	switch f.Status {
	case "active":
		conds = append(conds, "is_active = true")
	case "inactive":
		conds = append(conds, "is_active = false")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	// Total before pagination.
	countRow := r.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM users "+where, args...)
	var total int64
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	args = append(args, limit, offset)
	query := "SELECT " + userSelectColumns + " FROM users " + where +
		fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users, err := scanUserRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// PreregisterUser inserts an admin-invited row. The password hash
// stays empty so login(local) fails until the user completes
// registration; SSO callbacks promote the row by linking the IdP
// subject and setting password_hash to the placeholder marker.
func (r *Repo) PreregisterUser(ctx context.Context, invitedBy uuid.UUID, body *models.PreregisterUserRequest) (*models.User, error) {
	if body == nil || body.Email == "" || body.Name == "" {
		return nil, fmt.Errorf("email and name are required")
	}
	id := uuid.New()
	realm := "local"
	if body.Realm != nil && strings.TrimSpace(*body.Realm) != "" {
		realm = strings.TrimSpace(*body.Realm)
	}
	username := body.Username
	if username == nil || strings.TrimSpace(*username) == "" {
		// Default username to the email localpart.
		local := strings.SplitN(body.Email, "@", 2)[0]
		username = &local
	}
	attrs := body.Attributes
	if len(attrs) == 0 {
		attrs = []byte("{}")
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`INSERT INTO users
		   (id, email, username, name, password_hash, is_active, auth_source, realm,
		    organization_id, attributes, preregistered, invited_by)
		 VALUES ($1, $2, $3, $4, '', TRUE, $5, $5, $6, $7::jsonb, TRUE, $8)`,
		id, strings.TrimSpace(body.Email), strings.TrimSpace(*username), strings.TrimSpace(body.Name),
		realm, body.OrganizationID, attrs, invitedBy,
	); err != nil {
		return nil, fmt.Errorf("insert preregistered user: %w", err)
	}
	for _, roleName := range body.Roles {
		var roleID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE name = $1`, roleName).Scan(&roleID); err != nil {
			return nil, fmt.Errorf("lookup role %s: %w", roleName, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			id, roleID,
		); err != nil {
			return nil, fmt.Errorf("assign role: %w", err)
		}
	}
	for _, gid := range body.Groups {
		if _, err := tx.Exec(ctx,
			`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			gid, id,
		); err != nil {
			return nil, fmt.Errorf("assign group: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return r.FindUserByID(ctx, id)
}

// SoftDeleteUser marks a user as deleted_at = NOW() and inactivates
// the row. Refresh tokens are revoked in the same transaction (SG.4:
// "Disable or invalidate tokens for inactive/disabled users").
func (r *Repo) SoftDeleteUser(ctx context.Context, id uuid.UUID) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx,
		`UPDATE users SET deleted_at = $2, is_active = FALSE, updated_at = $2
		 WHERE id = $1 AND deleted_at IS NULL`,
		id, now,
	); err != nil {
		return fmt.Errorf("soft delete user: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = $2
		 WHERE user_id = $1 AND revoked_at IS NULL`,
		id, now,
	); err != nil {
		return fmt.Errorf("revoke refresh tokens: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE third_party_oauth_refresh_tokens SET revoked_at = $2
		 WHERE subject_user_id = $1 AND revoked_at IS NULL`,
		id, now,
	); err != nil {
		return fmt.Errorf("revoke oauth refresh tokens: %w", err)
	}
	return tx.Commit(ctx)
}

// UndeleteUser clears the deleted_at tombstone but leaves is_active
// = false so an admin must explicitly reactivate the user.
func (r *Repo) UndeleteUser(ctx context.Context, id uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE users SET deleted_at = NULL, updated_at = NOW()
		 WHERE id = $1`,
		id,
	)
	return err
}

// RevokeAllUserRefreshTokens marks every active refresh token for
// the user as revoked. SG.4 calls this whenever a user is
// deactivated (is_active flipped to false), soft-deleted, or has
// their account locked.
func (r *Repo) RevokeAllUserRefreshTokens(ctx context.Context, userID uuid.UUID, at time.Time) (int64, error) {
	tag, err := r.Pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = $2
		 WHERE user_id = $1 AND revoked_at IS NULL`,
		userID, at,
	)
	if err != nil {
		return 0, err
	}
	if _, err := r.Pool.Exec(ctx,
		`UPDATE third_party_oauth_refresh_tokens SET revoked_at = $2
		 WHERE subject_user_id = $1 AND revoked_at IS NULL`,
		userID, at,
	); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// StampLogin updates last_login_at / last_login_ip. Called from the
// Login handler in a follow-up patch; safe to invoke from SSO
// callbacks too.
func (r *Repo) StampLogin(ctx context.Context, userID uuid.UUID, at time.Time, ip string) error {
	var ipPtr *string
	if ip != "" {
		ipPtr = &ip
	}
	_, err := r.Pool.Exec(ctx,
		`UPDATE users SET last_login_at = $2, last_login_ip = $3, updated_at = $2
		 WHERE id = $1`,
		userID, at, ipPtr,
	)
	return err
}

// UserTokenSummary aggregates refresh-token state for the SG.4
// inspect endpoint. The caller turns this into the wire-shape
// `TokenSummary` (the repo result includes a few extra fields that
// help internal callers without leaking to wire).
type UserTokenSummary struct {
	ActiveCount   int
	RevokedCount  int
	NextExpiresAt *time.Time
}

// SummarizeUserTokens returns the refresh-token aggregate for the
// SG.4 inspect view.
func (r *Repo) SummarizeUserTokens(ctx context.Context, userID uuid.UUID) (*UserTokenSummary, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT
		   COUNT(*) FILTER (WHERE revoked_at IS NULL AND expires_at > NOW()) AS active,
		   COUNT(*) FILTER (WHERE revoked_at IS NOT NULL OR expires_at <= NOW()) AS revoked,
		   MIN(expires_at) FILTER (WHERE revoked_at IS NULL AND expires_at > NOW()) AS next_expires
		 FROM refresh_tokens
		 WHERE user_id = $1`,
		userID,
	)
	out := &UserTokenSummary{}
	if err := row.Scan(&out.ActiveCount, &out.RevokedCount, &out.NextExpiresAt); err != nil {
		return nil, err
	}
	return out, nil
}

// ListUserExternalIdentities returns every IdP binding row for a
// user. SG.4 inspect view uses this to surface realm membership.
func (r *Repo) ListUserExternalIdentities(ctx context.Context, userID uuid.UUID) ([]ExternalIdentity, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, user_id, provider, external_id, COALESCE(email, ''), last_login_at, created_at
		 FROM user_external_identities
		 WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ExternalIdentity, 0)
	for rows.Next() {
		e := ExternalIdentity{}
		if err := rows.Scan(&e.ID, &e.UserID, &e.Provider, &e.ExternalID, &e.Email, &e.LastLoginAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListUserGroups returns the group rows the user belongs to,
// projected to id+name+description for the SG.4 inspect view.
func (r *Repo) ListUserGroups(ctx context.Context, userID uuid.UUID) ([]models.Group, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT g.id, g.name, g.description, g.created_at
		 FROM groups g
		 INNER JOIN group_members gm ON gm.group_id = g.id
		 WHERE gm.user_id = $1
		 ORDER BY g.name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Group, 0)
	for rows.Next() {
		g := models.Group{}
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// CountActiveAPIKeys returns the count of non-revoked, non-expired
// api_keys for a user.
func (r *Repo) CountActiveAPIKeys(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := r.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM api_keys
		 WHERE user_id = $1 AND revoked_at IS NULL
		   AND expires_at IS NOT NULL AND expires_at > NOW()`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// scanUserRows is the shared streaming scan used by ListUsers and
// ListUsersFiltered.
func scanUserRows(rows pgx.Rows) ([]models.User, error) {
	out := make([]models.User, 0)
	for rows.Next() {
		u := models.User{}
		if err := rows.Scan(
			&u.ID, &u.Email, &u.Username, &u.Name, &u.PasswordHash,
			&u.IsActive, &u.AuthSource, &u.Realm, &u.MFAEnforced, &u.OrganizationID, &u.Attributes,
			&u.LastLoginAt, &u.LastLoginIP, &u.Preregistered, &u.InvitedBy, &u.DeletedAt,
			&u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateUser applies non-nil fields of `body`. SG.4 extended the
// patch surface to include username, realm, organization_id, and
// attributes.
func (r *Repo) UpdateUser(ctx context.Context, id uuid.UUID, body *models.UpdateUserRequest) (*models.User, error) {
	current, err := r.FindUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}
	name := current.Name
	if body.Name != nil {
		name = *body.Name
	}
	username := current.Username
	if body.Username != nil {
		if v := strings.TrimSpace(*body.Username); v != "" {
			username = &v
		}
	}
	realm := current.Realm
	if body.Realm != nil {
		if v := strings.TrimSpace(*body.Realm); v != "" {
			realm = v
		}
	}
	active := current.IsActive
	if body.IsActive != nil {
		active = *body.IsActive
	}
	mfa := current.MFAEnforced
	if body.MFAEnforced != nil {
		mfa = *body.MFAEnforced
	}
	orgID := current.OrganizationID
	if body.OrganizationID != nil {
		orgID = *body.OrganizationID
	}
	attrs := current.Attributes
	if body.Attributes != nil {
		attrs = *body.Attributes
	}
	if len(attrs) == 0 {
		attrs = []byte("{}")
	}
	_, err = r.Pool.Exec(ctx,
		`UPDATE users SET
		   name = $2, username = $3, realm = $4, is_active = $5, mfa_enforced = $6,
		   organization_id = $7, attributes = $8::jsonb, updated_at = NOW()
		 WHERE id = $1`,
		id, name, username, realm, active, mfa, orgID, attrs,
	)
	if err != nil {
		return nil, err
	}
	return r.FindUserByID(ctx, id)
}

// DeleteUser removes a user (cascades user_roles, group_members, api_keys, etc.).
func (r *Repo) DeleteUser(ctx context.Context, id uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	return err
}

// UpdatePasswordHash rewrites the argon2id password digest for one
// user. Used by the self-service POST /api/v1/auth/password handler
// after the caller has proven possession of the current password.
// Returns sql.ErrNoRows when id does not match a row so callers can
// translate the miss to a 404 / 401 of their choosing.
func (r *Repo) UpdatePasswordHash(ctx context.Context, id uuid.UUID, newHash string) error {
	tag, err := r.Pool.Exec(ctx,
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

// ListUserRoles returns roles assigned to a user.
func (r *Repo) ListUserRoles(ctx context.Context, userID uuid.UUID) ([]models.Role, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT r.id, r.name, r.description, r.created_at
		 FROM roles r
		 INNER JOIN user_roles ur ON ur.role_id = r.id
		 WHERE ur.user_id = $1
		 ORDER BY r.name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Role, 0)
	for rows.Next() {
		var role models.Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

// ListUserSecuritySnapshot returns the role names and permission keys
// a developer API token should inherit at creation time. The token
// keeps this snapshot until it expires or is revoked.
func (r *Repo) ListUserSecuritySnapshot(ctx context.Context, userID uuid.UUID) ([]string, []string, error) {
	roleRows, err := r.Pool.Query(ctx,
		`SELECT r.name
		 FROM roles r
		 INNER JOIN user_roles ur ON ur.role_id = r.id
		 WHERE ur.user_id = $1
		 ORDER BY r.name`,
		userID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer roleRows.Close()
	roles := make([]string, 0)
	for roleRows.Next() {
		var role string
		if err := roleRows.Scan(&role); err != nil {
			return nil, nil, err
		}
		roles = append(roles, role)
	}
	if err := roleRows.Err(); err != nil {
		return nil, nil, err
	}

	permRows, err := r.Pool.Query(ctx,
		`SELECT DISTINCT p.resource || ':' || p.action AS permission
		 FROM permissions p
		 INNER JOIN role_permissions rp ON rp.permission_id = p.id
		 INNER JOIN user_roles ur ON ur.role_id = rp.role_id
		 WHERE ur.user_id = $1
		 ORDER BY permission`,
		userID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer permRows.Close()
	permissions := make([]string, 0)
	for permRows.Next() {
		var permission string
		if err := permRows.Scan(&permission); err != nil {
			return nil, nil, err
		}
		permissions = append(permissions, permission)
	}
	if err := permRows.Err(); err != nil {
		return nil, nil, err
	}
	return roles, permissions, nil
}

// AssignRoleToUser is idempotent.
func (r *Repo) AssignRoleToUser(ctx context.Context, userID, roleID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, roleID,
	)
	return err
}

// RevokeRoleFromUser is a no-op when the assignment doesn't exist.
func (r *Repo) RevokeRoleFromUser(ctx context.Context, userID, roleID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2`,
		userID, roleID,
	)
	return err
}

// ─── Roles ──────────────────────────────────────────────────────────────

func (r *Repo) ListRoles(ctx context.Context) ([]models.Role, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, name, description, created_at FROM roles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Role, 0)
	for rows.Next() {
		var role models.Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

func (r *Repo) GetRole(ctx context.Context, id uuid.UUID) (*models.Role, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, name, description, created_at FROM roles WHERE id = $1`, id)
	role := &models.Role{}
	if err := row.Scan(&role.ID, &role.Name, &role.Description, &role.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return role, nil
}

func (r *Repo) CreateRole(ctx context.Context, body *models.CreateRoleRequest) (*models.Role, error) {
	id := uuid.New()
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO roles (id, name, description) VALUES ($1, $2, $3)`,
		id, body.Name, body.Description,
	)
	if err != nil {
		return nil, err
	}
	return r.GetRole(ctx, id)
}

func (r *Repo) UpdateRole(ctx context.Context, id uuid.UUID, body *models.UpdateRoleRequest) (*models.Role, error) {
	_, err := r.Pool.Exec(ctx,
		`UPDATE roles SET name = $2, description = $3 WHERE id = $1`,
		id, body.Name, body.Description,
	)
	if err != nil {
		return nil, err
	}
	return r.GetRole(ctx, id)
}

func (r *Repo) DeleteRole(ctx context.Context, id uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM roles WHERE id = $1`, id)
	return err
}

// ─── Permissions ────────────────────────────────────────────────────────

func (r *Repo) ListPermissions(ctx context.Context) ([]models.Permission, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, resource, action, created_at FROM permissions ORDER BY resource, action`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Permission, 0)
	for rows.Next() {
		var p models.Permission
		if err := rows.Scan(&p.ID, &p.Resource, &p.Action, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) CreatePermission(ctx context.Context, body *models.CreatePermissionRequest) (*models.Permission, error) {
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO permissions (resource, action) VALUES ($1, $2)
		 ON CONFLICT (resource, action) DO UPDATE SET resource = EXCLUDED.resource
		 RETURNING id, resource, action, created_at`,
		body.Resource, body.Action,
	)
	p := &models.Permission{}
	if err := row.Scan(&p.ID, &p.Resource, &p.Action, &p.CreatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

func (r *Repo) DeletePermission(ctx context.Context, id uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM permissions WHERE id = $1`, id)
	return err
}

func (r *Repo) AssignPermissionToRole(ctx context.Context, roleID, permID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO role_permissions (role_id, permission_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		roleID, permID,
	)
	return err
}

func (r *Repo) RevokePermissionFromRole(ctx context.Context, roleID, permID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`DELETE FROM role_permissions WHERE role_id = $1 AND permission_id = $2`,
		roleID, permID,
	)
	return err
}

// ─── Groups ─────────────────────────────────────────────────────────────

// groupSelectColumns is the canonical projection used by every
// groups-table SELECT.
const groupSelectColumns = `id, name, COALESCE(display_name, name) AS display_name,
	description, kind, realm, organization_id, attributes, rule_query,
	status, created_at, updated_at`

func (r *Repo) ListGroups(ctx context.Context) ([]models.Group, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+groupSelectColumns+`
		 FROM groups WHERE status = 'active'
		 ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGroupRows(rows)
}

// ListGroupsFiltered is the SG.5 search surface used by
// /groups/search. Supports case-insensitive substring on name +
// display_name + description, exact kind / realm / organization_id
// filters, status filter, offset pagination, and a total count.
func (r *Repo) ListGroupsFiltered(ctx context.Context, f *models.ListGroupsFilter) ([]models.Group, int64, error) {
	if f == nil {
		f = &models.ListGroupsFilter{}
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	args := make([]any, 0, 8)
	conds := make([]string, 0, 6)
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		conds = append(conds,
			fmt.Sprintf("(LOWER(name) LIKE $%d OR LOWER(COALESCE(display_name,'')) LIKE $%d OR LOWER(COALESCE(description,'')) LIKE $%d)",
				len(args), len(args), len(args)))
	}
	if kind := strings.TrimSpace(f.Kind); kind != "" {
		args = append(args, kind)
		conds = append(conds, fmt.Sprintf("kind = $%d", len(args)))
	}
	if realm := strings.TrimSpace(f.Realm); realm != "" {
		args = append(args, realm)
		conds = append(conds, fmt.Sprintf("realm = $%d", len(args)))
	}
	if f.OrganizationID != nil {
		args = append(args, *f.OrganizationID)
		conds = append(conds, fmt.Sprintf("organization_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(f.Status); status != "" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var total int64
	if err := r.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM groups "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count groups: %w", err)
	}
	args = append(args, limit, offset)
	query := "SELECT " + groupSelectColumns + " FROM groups " + where +
		fmt.Sprintf(" ORDER BY display_name LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	groups, err := scanGroupRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return groups, total, nil
}

func (r *Repo) GetGroup(ctx context.Context, id uuid.UUID) (*models.Group, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+groupSelectColumns+`
		 FROM groups WHERE id = $1`, id)
	g, err := scanGroupSingle(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

func (r *Repo) CreateGroup(ctx context.Context, body *models.CreateGroupRequest) (*models.Group, error) {
	if strings.TrimSpace(body.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	id := uuid.New()
	kind := models.GroupKindInternal
	if body.Kind != nil && strings.TrimSpace(*body.Kind) != "" {
		kind = strings.ToLower(strings.TrimSpace(*body.Kind))
	}
	if !isValidGroupKind(kind) {
		return nil, fmt.Errorf("kind must be 'internal', 'external', or 'rule_based'")
	}
	realm := "local"
	if body.Realm != nil && strings.TrimSpace(*body.Realm) != "" {
		realm = strings.TrimSpace(*body.Realm)
	}
	status := models.GroupStatusActive
	if body.Status != nil && strings.TrimSpace(*body.Status) != "" {
		status = strings.ToLower(strings.TrimSpace(*body.Status))
	}
	display := strings.TrimSpace(body.Name)
	if body.DisplayName != nil && strings.TrimSpace(*body.DisplayName) != "" {
		display = strings.TrimSpace(*body.DisplayName)
	}
	attrs := body.Attributes
	if len(attrs) == 0 {
		attrs = []byte("{}")
	}
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO groups
		   (id, name, display_name, description, kind, realm, organization_id,
		    attributes, rule_query, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)`,
		id, strings.TrimSpace(body.Name), display, body.Description, kind, realm,
		body.OrganizationID, attrs, jsonOrNil(body.RuleQuery), status,
	)
	if err != nil {
		return nil, fmt.Errorf("insert group: %w", err)
	}
	return r.GetGroup(ctx, id)
}

func (r *Repo) UpdateGroup(ctx context.Context, id uuid.UUID, body *models.UpdateGroupRequest) (*models.Group, error) {
	current, err := r.GetGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}
	name := current.Name
	if body.Name != nil {
		name = *body.Name
	}
	display := current.DisplayName
	if body.DisplayName != nil {
		display = *body.DisplayName
	}
	desc := current.Description
	if body.Description != nil {
		desc = *body.Description
	}
	kind := current.Kind
	if body.Kind != nil {
		kind = strings.ToLower(strings.TrimSpace(*body.Kind))
	}
	if !isValidGroupKind(kind) {
		return nil, fmt.Errorf("kind must be 'internal', 'external', or 'rule_based'")
	}
	realm := current.Realm
	if body.Realm != nil {
		realm = strings.TrimSpace(*body.Realm)
	}
	orgID := current.OrganizationID
	if body.OrganizationID != nil {
		orgID = *body.OrganizationID
	}
	attrs := current.Attributes
	if body.Attributes != nil {
		attrs = *body.Attributes
	}
	if len(attrs) == 0 {
		attrs = []byte("{}")
	}
	rule := current.RuleQuery
	if body.RuleQuery != nil {
		rule = *body.RuleQuery
	}
	status := current.Status
	if body.Status != nil {
		status = strings.ToLower(strings.TrimSpace(*body.Status))
	}
	_, err = r.Pool.Exec(ctx,
		`UPDATE groups SET
		   name = $2, display_name = $3, description = $4, kind = $5, realm = $6,
		   organization_id = $7, attributes = $8::jsonb, rule_query = $9, status = $10,
		   updated_at = NOW()
		 WHERE id = $1`,
		id, name, display, desc, kind, realm, orgID, attrs, jsonOrNil(rule), status,
	)
	if err != nil {
		return nil, err
	}
	return r.GetGroup(ctx, id)
}

func (r *Repo) DeleteGroup(ctx context.Context, id uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM groups WHERE id = $1`, id)
	return err
}

// AddGroupMember inserts (or refreshes the metadata of) one
// group_members row. SG.5: optionally time-bounded membership.
func (r *Repo) AddGroupMember(ctx context.Context, groupID, userID uuid.UUID, addedBy *uuid.UUID, expiresAt *time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id, added_by, expires_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (group_id, user_id) DO UPDATE
		   SET added_by   = COALESCE(EXCLUDED.added_by, group_members.added_by),
		       expires_at = EXCLUDED.expires_at`,
		groupID, userID, addedBy, expiresAt,
	)
	return err
}

func (r *Repo) RemoveGroupMember(ctx context.Context, groupID, userID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`DELETE FROM group_members WHERE group_id = $1 AND user_id = $2`,
		groupID, userID,
	)
	return err
}

// ListGroupMembers returns one row per direct member with the SG.5
// expiration columns.
func (r *Repo) ListGroupMembers(ctx context.Context, groupID uuid.UUID) ([]models.GroupMember, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT group_id, user_id, added_at, added_by, expires_at
		 FROM group_members
		 WHERE group_id = $1
		 ORDER BY added_at DESC`,
		groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.GroupMember, 0)
	for rows.Next() {
		m := models.GroupMember{}
		if err := rows.Scan(&m.GroupID, &m.UserID, &m.AddedAt, &m.AddedBy, &m.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountGroupMembers returns (direct, expiring) counts for the SG.5
// inspect view. `expiring` is the subset of direct members whose
// expires_at is non-null and in the future.
func (r *Repo) CountGroupMembers(ctx context.Context, groupID uuid.UUID) (int, int, error) {
	var direct, expiring int
	err := r.Pool.QueryRow(ctx,
		`SELECT
		   COUNT(*) AS direct,
		   COUNT(*) FILTER (WHERE expires_at IS NOT NULL AND expires_at > NOW()) AS expiring
		 FROM group_members WHERE group_id = $1`,
		groupID,
	).Scan(&direct, &expiring)
	return direct, expiring, err
}

// ─── Group admins (SG.5) ────────────────────────────────────────────────

func (r *Repo) ListGroupAdmins(ctx context.Context, groupID uuid.UUID) ([]models.GroupAdmin, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT group_id, user_id, scope, granted_by, created_at
		 FROM group_admins WHERE group_id = $1
		 ORDER BY created_at DESC`,
		groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.GroupAdmin, 0)
	for rows.Next() {
		a := models.GroupAdmin{}
		if err := rows.Scan(&a.GroupID, &a.UserID, &a.Scope, &a.GrantedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repo) UpsertGroupAdmin(ctx context.Context, groupID uuid.UUID, body *models.CreateGroupAdminRequest) (*models.GroupAdmin, error) {
	scope := models.GroupAdminScopeManage
	if body.Scope != nil && strings.TrimSpace(*body.Scope) != "" {
		scope = strings.ToLower(strings.TrimSpace(*body.Scope))
	}
	if scope != models.GroupAdminScopeManage && scope != models.GroupAdminScopeManageMembers {
		return nil, fmt.Errorf("scope must be 'manage' or 'manage_members'")
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO group_admins (group_id, user_id, scope, granted_by)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (group_id, user_id, scope) DO UPDATE
		   SET granted_by = EXCLUDED.granted_by
		 RETURNING group_id, user_id, scope, granted_by, created_at`,
		groupID, body.UserID, scope, body.GrantedBy,
	)
	a := &models.GroupAdmin{}
	if err := row.Scan(&a.GroupID, &a.UserID, &a.Scope, &a.GrantedBy, &a.CreatedAt); err != nil {
		return nil, err
	}
	return a, nil
}

func (r *Repo) DeleteGroupAdmin(ctx context.Context, groupID, userID uuid.UUID, scope string) error {
	_, err := r.Pool.Exec(ctx,
		`DELETE FROM group_admins
		 WHERE group_id = $1 AND user_id = $2 AND scope = $3`,
		groupID, userID, scope,
	)
	return err
}

// IsGroupAdmin returns true when `userID` carries any admin scope on
// `groupID`. SG.5: gateway for group-membership write authorization.
func (r *Repo) IsGroupAdmin(ctx context.Context, groupID, userID uuid.UUID) (bool, error) {
	var count int
	err := r.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_admins
		 WHERE group_id = $1 AND user_id = $2`, groupID, userID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ─── Nested group membership (SG.5) ─────────────────────────────────────

func (r *Repo) AddGroupNested(ctx context.Context, parentID, memberID uuid.UUID, addedBy *uuid.UUID) error {
	if parentID == memberID {
		return fmt.Errorf("a group cannot contain itself")
	}
	// Cheap cycle check: if memberID is already an ancestor of
	// parentID, the new edge would close a cycle. Walk up the
	// member's ancestors looking for parentID.
	cycle, err := r.groupCycleWouldClose(ctx, parentID, memberID)
	if err != nil {
		return err
	}
	if cycle {
		return fmt.Errorf("would close a nesting cycle")
	}
	_, err = r.Pool.Exec(ctx,
		`INSERT INTO group_nested_members (parent_group_id, member_group_id, added_by)
		 VALUES ($1, $2, $3)
		 ON CONFLICT DO NOTHING`,
		parentID, memberID, addedBy,
	)
	return err
}

func (r *Repo) RemoveGroupNested(ctx context.Context, parentID, memberID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`DELETE FROM group_nested_members
		 WHERE parent_group_id = $1 AND member_group_id = $2`,
		parentID, memberID,
	)
	return err
}

// ListGroupParents returns the groups that directly contain `groupID`.
func (r *Repo) ListGroupParents(ctx context.Context, groupID uuid.UUID) ([]models.GroupBrief, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT g.id, COALESCE(NULLIF(g.display_name,''), g.name)
		 FROM group_nested_members gnm
		 INNER JOIN groups g ON g.id = gnm.parent_group_id
		 WHERE gnm.member_group_id = $1
		 ORDER BY 2`,
		groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.GroupBrief, 0)
	for rows.Next() {
		var b models.GroupBrief
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListGroupChildren returns the groups directly contained by `groupID`.
func (r *Repo) ListGroupChildren(ctx context.Context, groupID uuid.UUID) ([]models.GroupBrief, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT g.id, COALESCE(NULLIF(g.display_name,''), g.name)
		 FROM group_nested_members gnm
		 INNER JOIN groups g ON g.id = gnm.member_group_id
		 WHERE gnm.parent_group_id = $1
		 ORDER BY 2`,
		groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.GroupBrief, 0)
	for rows.Next() {
		var b models.GroupBrief
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// groupCycleWouldClose reports whether adding parent->member would
// close a nesting cycle. Implemented with a CTE that walks up from
// `parent` looking for `member`.
func (r *Repo) groupCycleWouldClose(ctx context.Context, parent, member uuid.UUID) (bool, error) {
	var hit bool
	err := r.Pool.QueryRow(ctx,
		`WITH RECURSIVE ancestors(g) AS (
		   SELECT $1::uuid
		   UNION ALL
		   SELECT gnm.parent_group_id
		   FROM group_nested_members gnm
		   INNER JOIN ancestors a ON a.g = gnm.member_group_id
		 )
		 SELECT EXISTS(SELECT 1 FROM ancestors WHERE g = $2::uuid)`,
		parent, member,
	).Scan(&hit)
	return hit, err
}

// ─── helpers ────────────────────────────────────────────────────────────

func scanGroupSingle(row pgx.Row) (*models.Group, error) {
	g := &models.Group{}
	var attrs, rule []byte
	if err := row.Scan(
		&g.ID, &g.Name, &g.DisplayName, &g.Description, &g.Kind, &g.Realm,
		&g.OrganizationID, &attrs, &rule, &g.Status, &g.CreatedAt, &g.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(attrs) == 0 {
		attrs = []byte("{}")
	}
	g.Attributes = attrs
	g.RuleQuery = rule
	return g, nil
}

func scanGroupRows(rows pgx.Rows) ([]models.Group, error) {
	out := make([]models.Group, 0)
	for rows.Next() {
		g := models.Group{}
		var attrs, rule []byte
		if err := rows.Scan(
			&g.ID, &g.Name, &g.DisplayName, &g.Description, &g.Kind, &g.Realm,
			&g.OrganizationID, &attrs, &rule, &g.Status, &g.CreatedAt, &g.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(attrs) == 0 {
			attrs = []byte("{}")
		}
		g.Attributes = attrs
		g.RuleQuery = rule
		out = append(out, g)
	}
	return out, rows.Err()
}

func isValidGroupKind(kind string) bool {
	switch kind {
	case models.GroupKindInternal, models.GroupKindExternal, models.GroupKindRuleBased:
		return true
	default:
		return false
	}
}

// jsonOrNil collapses an empty / empty-object JSON value to a NULL
// SQL parameter so the column reflects "no rule" with NULL.
func jsonOrNil(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	return b
}

// ─── API keys ───────────────────────────────────────────────────────────

const apiKeySelectColumns = `id, user_id, name, prefix, scopes,
	permissions_snapshot, roles_snapshot, warning,
	CASE
		WHEN revoked_at IS NOT NULL THEN 'revoked'
		WHEN expires_at IS NULL OR expires_at <= NOW() THEN 'expired'
		ELSE 'active'
	END AS status,
	last_used_at, expires_at, created_at, revoked_at`

func (r *Repo) ListAPIKeys(ctx context.Context, userID uuid.UUID) ([]models.APIKey, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+apiKeySelectColumns+`
		 FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.APIKey, 0)
	for rows.Next() {
		var k models.APIKey
		if err := scanAPIKey(&k, rows); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// CreateAPIKey persists a hashed token. Returns the row + caller is
// expected to render the plaintext (which it generated).
func (r *Repo) CreateAPIKey(ctx context.Context, userID uuid.UUID, name, keyHash, prefix string, scopes, permissionsSnapshot, rolesSnapshot []string, expiresAt time.Time) (*models.APIKey, error) {
	id := uuid.New()
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO api_keys
			(id, user_id, name, key_hash, prefix, scopes, permissions_snapshot, roles_snapshot, expires_at, warning)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, userID, name, keyHash, prefix, scopes, permissionsSnapshot, rolesSnapshot, expiresAt, models.DeveloperAPIKeyWarning,
	)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(ctx,
		`SELECT `+apiKeySelectColumns+`
		 FROM api_keys WHERE id = $1`, id)
	k := &models.APIKey{}
	if err := scanAPIKey(k, row); err != nil {
		return nil, err
	}
	return k, nil
}

func (r *Repo) RevokeAPIKey(ctx context.Context, userID, id uuid.UUID, at time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = $3 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		id, userID, at,
	)
	return err
}

// FindUsableAPIKeyByHash resolves an opaque developer API key to the
// persisted key and active user. Keys are unusable after explicit
// revocation, expiry, user disablement/deletion, or 30 days without a
// successful login by the owning user.
func (r *Repo) FindUsableAPIKeyByHash(ctx context.Context, keyHash string, now time.Time) (*models.APIKey, *models.User, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT
			k.id, k.user_id, k.name, k.prefix, k.scopes,
			k.permissions_snapshot, k.roles_snapshot, k.warning,
			CASE
				WHEN k.revoked_at IS NOT NULL THEN 'revoked'
				WHEN k.expires_at IS NULL OR k.expires_at <= $2 THEN 'expired'
				ELSE 'active'
			END AS status,
			k.last_used_at, k.expires_at, k.created_at, k.revoked_at,
			u.id, u.email, u.username, u.name, u.password_hash,
			u.is_active, u.auth_source, u.realm, u.mfa_enforced, u.organization_id, u.attributes,
			u.last_login_at, u.last_login_ip, u.preregistered, u.invited_by, u.deleted_at,
			u.created_at, u.updated_at
		 FROM api_keys k
		 INNER JOIN users u ON u.id = k.user_id
		 WHERE k.key_hash = $1
		   AND k.revoked_at IS NULL
		   AND k.expires_at IS NOT NULL
		   AND k.expires_at > $2
		   AND u.is_active = TRUE
		   AND u.deleted_at IS NULL
		   AND u.last_login_at IS NOT NULL
		   AND u.last_login_at >= ($2::timestamptz - INTERVAL '30 days')`,
		keyHash, now,
	)
	key := &models.APIKey{}
	user := &models.User{}
	err := row.Scan(
		&key.ID, &key.UserID, &key.Name, &key.Prefix, &key.Scopes,
		&key.PermissionsSnapshot, &key.RolesSnapshot, &key.Warning, &key.Status,
		&key.LastUsedAt, &key.ExpiresAt, &key.CreatedAt, &key.RevokedAt,
		&user.ID, &user.Email, &user.Username, &user.Name, &user.PasswordHash,
		&user.IsActive, &user.AuthSource, &user.Realm, &user.MFAEnforced, &user.OrganizationID, &user.Attributes,
		&user.LastLoginAt, &user.LastLoginIP, &user.Preregistered, &user.InvitedBy, &user.DeletedAt,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if _, err := r.Pool.Exec(ctx, `UPDATE api_keys SET last_used_at = $2 WHERE id = $1`, key.ID, now); err != nil {
		return nil, nil, err
	}
	key.LastUsedAt = &now
	return key, user, nil
}

type apiKeyScanner interface {
	Scan(dest ...any) error
}

func scanAPIKey(k *models.APIKey, row apiKeyScanner) error {
	return row.Scan(
		&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.Scopes,
		&k.PermissionsSnapshot, &k.RolesSnapshot, &k.Warning, &k.Status,
		&k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt, &k.RevokedAt,
	)
}
