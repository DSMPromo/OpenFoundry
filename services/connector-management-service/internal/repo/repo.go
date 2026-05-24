// Package repo holds SQL queries + embedded migrations for
// connector-management-service.
package repo

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

type Repo struct{ Pool *pgxpool.Pool }

const connectionSelect = `SELECT id, name, connector_type, config, status,
		owner_id, last_sync_at, created_at, updated_at FROM connections`

func (r *Repo) ListConnections(ctx context.Context, ownerID *uuid.UUID) ([]models.Connection, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if ownerID != nil {
		rows, err = r.Pool.Query(ctx, connectionSelect+` WHERE `+sourceAccessSQL("connections", "$1", "$2", "$3")+` ORDER BY created_at DESC LIMIT 500`,
			*ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	} else {
		rows, err = r.Pool.Query(ctx, connectionSelect+` ORDER BY created_at DESC LIMIT 500`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Connection, 0)
	for rows.Next() {
		v, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetConnection(ctx context.Context, id uuid.UUID) (*models.Connection, error) {
	row := r.Pool.QueryRow(ctx, connectionSelect+` WHERE id = $1`, id)
	v, err := scanConnection(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateConnection(ctx context.Context, body *models.CreateConnectionRequest, ownerID uuid.UUID) (*models.Connection, error) {
	id := uuid.New()
	cfg := body.Config
	if len(cfg) == 0 {
		cfg = []byte(`{}`)
	}
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO connections (id, name, connector_type, config, owner_id)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, name, connector_type, config, status, owner_id,
		           last_sync_at, created_at, updated_at`,
		id, strings.TrimSpace(body.Name), body.ConnectorType, cfg, ownerID,
	)
	return scanConnection(row)
}

func (r *Repo) UpdateConnection(ctx context.Context, id uuid.UUID, body *models.UpdateConnectionRequest) (*models.Connection, error) {
	current, err := r.GetConnection(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	name := current.Name
	if body.Name != nil {
		name = *body.Name
	}
	cfg := current.Config
	if len(body.Config) > 0 {
		cfg = body.Config
	}
	status := current.Status
	if body.Status != nil {
		status = *body.Status
	}
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE connections SET name = $2, config = $3, status = $4, updated_at = $5
		 WHERE id = $1
		 RETURNING id, name, connector_type, config, status, owner_id,
		           last_sync_at, created_at, updated_at`,
		id, name, cfg, status, time.Now().UTC(),
	)
	return scanConnection(row)
}

func (r *Repo) DeleteConnection(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM connections WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

// MigrateConnectionToFoundryWorker snapshots the source's current config into
// `previous_config_snapshot`, stamps `migrated_at = NOW()`, and merges
// `worker = "foundry"` into the active config. Callers have a 30-day window
// to revert by copying `previous_config_snapshot` back over `config`.
func (r *Repo) MigrateConnectionToFoundryWorker(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.Connection, error) {
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE connections
		 SET previous_config_snapshot = config,
		     migrated_at = NOW(),
		     config = config || '{"worker": "foundry"}'::jsonb,
		     updated_at = NOW()
		 WHERE id = $1 AND owner_id = $2
		 RETURNING id, name, connector_type, config, status,
		           owner_id, last_sync_at, created_at, updated_at`,
		id, ownerID,
	)
	v, err := scanConnection(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

type rowLikeT interface{ Scan(...any) error }

func scanConnection(r rowLikeT) (*models.Connection, error) {
	v := &models.Connection{}
	if err := r.Scan(&v.ID, &v.Name, &v.ConnectorType, &v.Config, &v.Status,
		&v.OwnerID, &v.LastSyncAt, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

func (r *Repo) GetConnectionForOwner(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.Connection, error) {
	row := r.Pool.QueryRow(ctx, connectionSelect+` WHERE id = $1 AND `+sourceAccessSQL("connections", "$2", "$3", "$4"),
		id, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	v, err := scanConnection(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func sourceAccessRoleNames(required models.SourcePermissionRole) []string {
	return models.SourcePermissionRoleStrings(models.RequiredSourceAccessRoles(required))
}

func sourceAccessSQL(sourceAlias string, actorUUIDParam string, actorTextParam string, rolesParam string) string {
	return fmt.Sprintf(`(%s.owner_id = %s OR EXISTS (
		SELECT 1 FROM source_permission_grants g
		 WHERE g.source_id = %s.id
		   AND g.principal_type IN ('user','service_account')
		   AND g.principal_id = %s
		   AND g.roles && %s::text[]
		   AND (g.expires_at IS NULL OR g.expires_at > NOW())
	))`, sourceAlias, actorUUIDParam, sourceAlias, actorTextParam, rolesParam)
}

func (r *Repo) CheckSourceRole(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID, role models.SourcePermissionRole) (bool, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM connections c WHERE c.id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+`)`,
		sourceID, actorID, actorID.String(), sourceAccessRoleNames(role))
	var allowed bool
	if err := row.Scan(&allowed); err != nil {
		return false, err
	}
	return allowed, nil
}

func (r *Repo) GetSourceGovernance(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID) (*models.SourceGovernance, error) {
	conn, err := r.GetConnectionForOwner(ctx, sourceID, actorID)
	if err != nil || conn == nil {
		return nil, err
	}
	grants, err := r.listSourcePermissionGrants(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	visibility, err := r.getSourceVisibilityPolicy(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	audit, err := r.ListSourceGovernanceAudit(ctx, sourceID, actorID, 50)
	if err != nil {
		return nil, err
	}
	effective := []models.SourcePermissionRole{}
	if conn.OwnerID == actorID {
		effective = models.AllSourcePermissionRoles()
	} else {
		now := time.Now().UTC()
		for _, grant := range grants {
			if grant.PrincipalID != actorID.String() {
				continue
			}
			if grant.ExpiresAt != nil && grant.ExpiresAt.Before(now) {
				continue
			}
			effective = append(effective, grant.Roles...)
		}
		effective = models.ExpandSourcePermissionRoles(effective)
	}
	return &models.SourceGovernance{
		SourceID:                 sourceID,
		SourceRID:                models.SourceRIDForConnection(sourceID),
		OwnerID:                  conn.OwnerID,
		RoleDefinitions:          models.SourcePermissionRoleDefinitions(),
		EffectiveRoles:           models.NormalizeSourcePermissionRoles(effective),
		PermissionGrants:         grants,
		Visibility:               visibility,
		OutputDatasetPermissions: []models.SourceOutputDatasetPermission{},
		AuditEvents:              audit,
		Warnings:                 models.SourceGovernanceWarnings(visibility),
	}, nil
}

func (r *Repo) UpdateSourceGovernance(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID, body *models.UpdateSourceGovernanceRequest) (*models.SourceGovernance, error) {
	allowed, err := r.CheckSourceRole(ctx, sourceID, actorID, models.SourceRoleOwner)
	if err != nil || !allowed {
		return nil, err
	}
	if body == nil {
		body = &models.UpdateSourceGovernanceRequest{}
	}
	now := time.Now().UTC()
	grants := models.NormalizeSourcePermissionGrants(body.PermissionGrants, sourceID, actorID, now)
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM source_permission_grants WHERE source_id = $1`, sourceID); err != nil {
		return nil, err
	}
	for _, grant := range grants {
		if _, err := tx.Exec(ctx,
			`INSERT INTO source_permission_grants
				(id, source_id, principal_id, principal_type, principal_name, roles, granted_by, reason, expires_at, granted_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			uuid.New(), sourceID, grant.PrincipalID, grant.PrincipalType, grant.PrincipalName,
			models.SourcePermissionRoleStrings(grant.Roles), grant.GrantedBy, grant.Reason, grant.ExpiresAt, grant.GrantedAt, now); err != nil {
			return nil, err
		}
	}
	visibility := models.DefaultSourceVisibilityPolicy()
	if body.Visibility != nil {
		visibility = models.NormalizeSourceVisibilityPolicy(*body.Visibility)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO source_visibility_policies
			(source_id, source_visibility_roles, credential_visibility_roles, external_sample_visibility_roles,
			 output_dataset_permission_roles, credential_values_visible, external_samples_persisted,
			 output_dataset_permissions_enforced, output_dataset_permission_system, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (source_id) DO UPDATE SET
		    source_visibility_roles = EXCLUDED.source_visibility_roles,
		    credential_visibility_roles = EXCLUDED.credential_visibility_roles,
		    external_sample_visibility_roles = EXCLUDED.external_sample_visibility_roles,
		    output_dataset_permission_roles = EXCLUDED.output_dataset_permission_roles,
		    credential_values_visible = EXCLUDED.credential_values_visible,
		    external_samples_persisted = EXCLUDED.external_samples_persisted,
		    output_dataset_permissions_enforced = EXCLUDED.output_dataset_permissions_enforced,
		    output_dataset_permission_system = EXCLUDED.output_dataset_permission_system,
		    updated_at = EXCLUDED.updated_at`,
		sourceID,
		models.SourcePermissionRoleStrings(visibility.SourceVisibilityRoles),
		models.SourcePermissionRoleStrings(visibility.CredentialVisibilityRoles),
		models.SourcePermissionRoleStrings(visibility.ExternalSampleVisibilityRoles),
		visibility.OutputDatasetPermissionRoles,
		visibility.CredentialValuesVisible,
		visibility.ExternalSamplesPersisted,
		visibility.OutputDatasetPermissionsEnforced,
		visibility.OutputDatasetPermissionSystem,
		now); err != nil {
		return nil, err
	}
	metadata := map[string]any{"grant_count": len(grants), "reason": strings.TrimSpace(body.Reason)}
	audit := models.NormalizeSourceGovernanceAuditRequest(models.RecordSourceGovernanceAuditRequest{
		SourceID: sourceID, ActorID: &actorID, EventType: "permission_change", Action: "update_source_governance",
		Result: "succeeded", Roles: []models.SourcePermissionRole{models.SourceRoleOwner}, Message: "Source governance updated", Metadata: metadata,
	})
	if err := insertSourceGovernanceAudit(ctx, tx, audit); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetSourceGovernance(ctx, sourceID, actorID)
}

func (r *Repo) ListSourceGovernanceAudit(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID, limit int) ([]models.SourceGovernanceAuditEvent, error) {
	allowed, err := r.CheckSourceRole(ctx, sourceID, actorID, models.SourceRoleView)
	if err != nil || !allowed {
		return []models.SourceGovernanceAuditEvent{}, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT id, source_id, actor_id, event_type, action, result, principal_id, principal_type, roles,
		        capability, job_rid, downstream_resource_rid, message, metadata, created_at
		   FROM source_governance_audit_events
		  WHERE source_id = $1
		  ORDER BY created_at DESC
		  LIMIT $2`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SourceGovernanceAuditEvent{}
	for rows.Next() {
		event, err := scanSourceGovernanceAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *Repo) RecordSourceGovernanceAudit(ctx context.Context, body models.RecordSourceGovernanceAuditRequest) (*models.SourceGovernanceAuditEvent, error) {
	body = models.NormalizeSourceGovernanceAuditRequest(body)
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO source_governance_audit_events
			(id, source_id, actor_id, event_type, action, result, principal_id, principal_type, roles,
			 capability, job_rid, downstream_resource_rid, message, metadata)
		 SELECT $1, c.id, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb
		   FROM connections c WHERE c.id = $2
		 RETURNING id, source_id, actor_id, event_type, action, result, principal_id, principal_type, roles,
		           capability, job_rid, downstream_resource_rid, message, metadata, created_at`,
		uuid.New(), body.SourceID, body.ActorID, body.EventType, body.Action, body.Result, body.PrincipalID,
		body.PrincipalType, models.SourcePermissionRoleStrings(body.Roles), body.Capability, body.JobRID,
		body.DownstreamResourceRID, body.Message, jsonMap(body.Metadata))
	event, err := scanSourceGovernanceAudit(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

type auditInserter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertSourceGovernanceAudit(ctx context.Context, exec auditInserter, body models.RecordSourceGovernanceAuditRequest) error {
	body = models.NormalizeSourceGovernanceAuditRequest(body)
	_, err := exec.Exec(ctx,
		`INSERT INTO source_governance_audit_events
			(id, source_id, actor_id, event_type, action, result, principal_id, principal_type, roles,
			 capability, job_rid, downstream_resource_rid, message, metadata)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb)`,
		uuid.New(), body.SourceID, body.ActorID, body.EventType, body.Action, body.Result, body.PrincipalID,
		body.PrincipalType, models.SourcePermissionRoleStrings(body.Roles), body.Capability, body.JobRID,
		body.DownstreamResourceRID, body.Message, jsonMap(body.Metadata))
	return err
}

func (r *Repo) listSourcePermissionGrants(ctx context.Context, sourceID uuid.UUID) ([]models.SourcePermissionGrant, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, source_id, principal_id, principal_type, principal_name, roles,
		        granted_by, reason, expires_at, granted_at
		   FROM source_permission_grants
		  WHERE source_id = $1
		  ORDER BY principal_type, principal_id`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SourcePermissionGrant{}
	for rows.Next() {
		var grant models.SourcePermissionGrant
		var roles []string
		if err := rows.Scan(&grant.ID, &grant.SourceID, &grant.PrincipalID, &grant.PrincipalType,
			&grant.PrincipalName, &roles, &grant.GrantedBy, &grant.Reason, &grant.ExpiresAt, &grant.GrantedAt); err != nil {
			return nil, err
		}
		grant.Roles = models.SourcePermissionRolesFromStrings(roles)
		out = append(out, grant)
	}
	return out, rows.Err()
}

func (r *Repo) getSourceVisibilityPolicy(ctx context.Context, sourceID uuid.UUID) (models.SourceVisibilityPolicy, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT source_visibility_roles, credential_visibility_roles, external_sample_visibility_roles,
		        output_dataset_permission_roles, credential_values_visible, external_samples_persisted,
		        output_dataset_permissions_enforced, output_dataset_permission_system
		   FROM source_visibility_policies
		  WHERE source_id = $1`, sourceID)
	var policy models.SourceVisibilityPolicy
	var sourceRoles, credentialRoles, sampleRoles []string
	if err := row.Scan(&sourceRoles, &credentialRoles, &sampleRoles, &policy.OutputDatasetPermissionRoles,
		&policy.CredentialValuesVisible, &policy.ExternalSamplesPersisted,
		&policy.OutputDatasetPermissionsEnforced, &policy.OutputDatasetPermissionSystem); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.DefaultSourceVisibilityPolicy(), nil
		}
		return policy, err
	}
	policy.SourceVisibilityRoles = models.SourcePermissionRolesFromStrings(sourceRoles)
	policy.CredentialVisibilityRoles = models.SourcePermissionRolesFromStrings(credentialRoles)
	policy.ExternalSampleVisibilityRoles = models.SourcePermissionRolesFromStrings(sampleRoles)
	return models.NormalizeSourceVisibilityPolicy(policy), nil
}

func jsonMap(value map[string]any) []byte {
	if value == nil {
		return []byte(`{}`)
	}
	out, err := json.Marshal(value)
	if err != nil {
		return []byte(`{}`)
	}
	return out
}

func scanSourceGovernanceAudit(r rowLikeT) (models.SourceGovernanceAuditEvent, error) {
	var event models.SourceGovernanceAuditEvent
	var roles []string
	var metadata []byte
	if err := r.Scan(&event.ID, &event.SourceID, &event.ActorID, &event.EventType, &event.Action,
		&event.Result, &event.PrincipalID, &event.PrincipalType, &roles, &event.Capability,
		&event.JobRID, &event.DownstreamResourceRID, &event.Message, &metadata, &event.CreatedAt); err != nil {
		return event, err
	}
	event.Roles = models.SourcePermissionRolesFromStrings(roles)
	if len(metadata) > 0 && string(metadata) != "null" {
		_ = json.Unmarshal(metadata, &event.Metadata)
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	return event, nil
}

func (r *Repo) ListCredentials(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.CredentialResponse, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT sc.id, sc.source_id, sc.kind, sc.fingerprint, sc.validation_status, sc.last_validated_at, sc.expires_at, sc.created_at
		 FROM source_credentials sc JOIN connections c ON c.id = sc.source_id
		 WHERE sc.source_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+` ORDER BY sc.created_at DESC`,
		sourceID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleCodeImport))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CredentialResponse, 0)
	for rows.Next() {
		v := models.CredentialResponse{}
		if err := rows.Scan(&v.ID, &v.SourceID, &v.Kind, &v.Fingerprint, &v.ValidationStatus, &v.LastValidatedAt, &v.ExpiresAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repo) SetCredential(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, kind string, ciphertext []byte, fingerprint string) (*models.CredentialResponse, error) {
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO source_credentials (id, source_id, kind, secret_ciphertext, fingerprint, validation_status, last_validated_at, expires_at)
		 SELECT $1, c.id, $3, $4, $5, 'untested', NULL, NULL FROM connections c WHERE c.id = $2 AND `+sourceAccessSQL("c", "$6", "$7", "$8")+`
		 ON CONFLICT (source_id, kind) DO UPDATE
		   SET secret_ciphertext = EXCLUDED.secret_ciphertext,
		       fingerprint = EXCLUDED.fingerprint,
		       validation_status = EXCLUDED.validation_status,
		       last_validated_at = EXCLUDED.last_validated_at,
		       expires_at = EXCLUDED.expires_at,
		       created_at = NOW()
		 RETURNING id, source_id, kind, fingerprint, validation_status, last_validated_at, expires_at, created_at`,
		uuid.New(), sourceID, kind, ciphertext, fingerprint, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleEdit))
	v := &models.CredentialResponse{}
	if err := row.Scan(&v.ID, &v.SourceID, &v.Kind, &v.Fingerprint, &v.ValidationStatus, &v.LastValidatedAt, &v.ExpiresAt, &v.CreatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

func (r *Repo) ListSourcePolicies(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.SourcePolicyBindingResponse, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT b.source_id, b.policy_id, b.kind
		 FROM source_policy_bindings b JOIN connections c ON c.id = b.source_id
		 WHERE b.source_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+` ORDER BY b.created_at DESC`,
		sourceID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.SourcePolicyBindingResponse, 0)
	for rows.Next() {
		v := models.SourcePolicyBindingResponse{}
		if err := rows.Scan(&v.SourceID, &v.PolicyID, &v.Kind); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repo) AttachPolicy(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, policyID uuid.UUID, kind string) (*models.SourcePolicyBindingResponse, error) {
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO source_policy_bindings (source_id, policy_id, kind)
		 SELECT c.id, $2, $3 FROM connections c WHERE c.id = $1 AND `+sourceAccessSQL("c", "$4", "$5", "$6")+`
		 ON CONFLICT (source_id, policy_id) DO UPDATE SET kind = EXCLUDED.kind
		 RETURNING source_id, policy_id, kind`, sourceID, policyID, kind, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleEdit))
	v := &models.SourcePolicyBindingResponse{}
	if err := row.Scan(&v.SourceID, &v.PolicyID, &v.Kind); err != nil {
		return nil, err
	}
	return v, nil
}

func (r *Repo) DetachPolicy(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, policyID uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx,
		`DELETE FROM source_policy_bindings b USING connections c
		 WHERE b.source_id = c.id AND b.source_id = $1 AND `+sourceAccessSQL("c", "$2", "$4", "$5")+` AND b.policy_id = $3`,
		sourceID, ownerID, policyID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleEdit))
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func (r *Repo) GetSourceCodeImport(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) (*models.SourceCodeImport, error) {
	conn, err := r.GetConnectionForOwner(ctx, sourceID, ownerID)
	if err != nil || conn == nil {
		return nil, err
	}
	settings, err := r.loadSourceCodeImportSettings(ctx, conn)
	if err != nil {
		return nil, err
	}
	return r.buildSourceCodeImport(ctx, conn, ownerID, settings, nil)
}

func (r *Repo) UpdateSourceCodeImport(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, body *models.UpdateSourceCodeImportRequest) (*models.SourceCodeImport, error) {
	allowed, err := r.CheckSourceRole(ctx, sourceID, ownerID, models.SourceRoleCodeImport)
	if err != nil || !allowed {
		return nil, err
	}
	conn, err := r.GetConnection(ctx, sourceID)
	if err != nil || conn == nil {
		return nil, err
	}
	current, err := r.loadSourceCodeImportSettings(ctx, conn)
	if err != nil {
		return nil, err
	}
	enabled := current.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	friendlyName := current.FriendlyName
	if body.FriendlyName != nil {
		friendlyName = strings.TrimSpace(*body.FriendlyName)
	}
	if friendlyName == "" {
		friendlyName = conn.Name
	}
	pythonIdentifier := current.PythonIdentifier
	if body.PythonIdentifier != nil {
		pythonIdentifier = models.PythonIdentifier(*body.PythonIdentifier, friendlyName)
	}
	if pythonIdentifier == "" {
		pythonIdentifier = models.PythonIdentifier(friendlyName, conn.Name)
	}
	codeRepositories := current.CodeRepositories
	if body.CodeRepositories != nil {
		codeRepositories = models.NormalizeCodeRepositories(body.CodeRepositories, pythonIdentifier)
	}
	exportControls := current.ExportControls
	if body.ExportControls != nil {
		exportControls = models.NormalizeExportControls(*body.ExportControls)
	}
	reposJSON, err := json.Marshal(codeRepositories)
	if err != nil {
		return nil, err
	}
	controlsJSON, err := json.Marshal(exportControls)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO source_code_imports (source_id, enabled, friendly_name, python_identifier, code_repositories, export_controls)
			 SELECT c.id, $3, $4, $5, $6::jsonb, $7::jsonb FROM connections c WHERE c.id = $1 AND `+sourceAccessSQL("c", "$2", "$8", "$9")+`
		 ON CONFLICT (source_id) DO UPDATE SET enabled = EXCLUDED.enabled,
		     friendly_name = EXCLUDED.friendly_name,
		     python_identifier = EXCLUDED.python_identifier,
		     code_repositories = EXCLUDED.code_repositories,
		     export_controls = EXCLUDED.export_controls,
		     updated_at = NOW()
		 RETURNING enabled, friendly_name, python_identifier, code_repositories, export_controls, created_at, updated_at`,
		sourceID, ownerID, enabled, friendlyName, pythonIdentifier, reposJSON, controlsJSON,
		ownerID.String(), sourceAccessRoleNames(models.SourceRoleCodeImport),
	)
	settings, err := scanSourceCodeImportSettings(conn, row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.buildSourceCodeImport(ctx, conn, ownerID, settings, nil)
}

func (r *Repo) ResolveSourceCodeImportBuildStart(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, body *models.ResolveSourceCodeImportBuildRequest) (*models.SourceCodeImportBuildResolution, error) {
	allowed, err := r.CheckSourceRole(ctx, sourceID, ownerID, models.SourceRoleCodeImport)
	if err != nil || !allowed {
		return nil, err
	}
	conn, err := r.GetConnection(ctx, sourceID)
	if err != nil || conn == nil {
		return nil, err
	}
	settings, err := r.loadSourceCodeImportSettings(ctx, conn)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, fmt.Errorf("source is not approved for code imports")
	}
	resolution, err := r.buildSourceCodeImportResolution(ctx, conn, ownerID, settings, body)
	if err != nil {
		return nil, err
	}
	return &resolution, nil
}

type sourceCodeImportSettings struct {
	Enabled          bool
	FriendlyName     string
	PythonIdentifier string
	CodeRepositories []models.CodeRepositorySourceImport
	ExportControls   models.ExportControls
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (r *Repo) loadSourceCodeImportSettings(ctx context.Context, conn *models.Connection) (sourceCodeImportSettings, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT enabled, friendly_name, python_identifier, code_repositories, export_controls, created_at, updated_at
		   FROM source_code_imports WHERE source_id = $1`, conn.ID)
	settings, err := scanSourceCodeImportSettings(conn, row)
	if errors.Is(err, pgx.ErrNoRows) {
		now := conn.UpdatedAt
		if now.IsZero() {
			now = time.Now().UTC()
		}
		friendlyName := strings.TrimSpace(conn.Name)
		pythonIdentifier := models.PythonIdentifier(friendlyName, conn.ConnectorType)
		return sourceCodeImportSettings{
			FriendlyName:     friendlyName,
			PythonIdentifier: pythonIdentifier,
			CodeRepositories: []models.CodeRepositorySourceImport{},
			ExportControls:   models.NormalizeExportControls(models.ExportControls{}),
			CreatedAt:        conn.CreatedAt,
			UpdatedAt:        now,
		}, nil
	}
	return settings, err
}

func scanSourceCodeImportSettings(conn *models.Connection, row rowLikeT) (sourceCodeImportSettings, error) {
	var settings sourceCodeImportSettings
	var repositoriesJSON, exportControlsJSON []byte
	if err := row.Scan(&settings.Enabled, &settings.FriendlyName, &settings.PythonIdentifier, &repositoriesJSON, &exportControlsJSON, &settings.CreatedAt, &settings.UpdatedAt); err != nil {
		return settings, err
	}
	settings.FriendlyName = strings.TrimSpace(settings.FriendlyName)
	if settings.FriendlyName == "" && conn != nil {
		settings.FriendlyName = conn.Name
	}
	settings.PythonIdentifier = models.PythonIdentifier(settings.PythonIdentifier, settings.FriendlyName)
	if len(repositoriesJSON) > 0 && string(repositoriesJSON) != "null" {
		_ = json.Unmarshal(repositoriesJSON, &settings.CodeRepositories)
	}
	settings.CodeRepositories = models.NormalizeCodeRepositories(settings.CodeRepositories, settings.PythonIdentifier)
	if len(exportControlsJSON) > 0 && string(exportControlsJSON) != "null" {
		_ = json.Unmarshal(exportControlsJSON, &settings.ExportControls)
	}
	settings.ExportControls = models.NormalizeExportControls(settings.ExportControls)
	return settings, nil
}

func (r *Repo) buildSourceCodeImport(ctx context.Context, conn *models.Connection, ownerID uuid.UUID, settings sourceCodeImportSettings, buildRequest *models.ResolveSourceCodeImportBuildRequest) (*models.SourceCodeImport, error) {
	resolution, err := r.buildSourceCodeImportResolution(ctx, conn, ownerID, settings, buildRequest)
	if err != nil {
		return nil, err
	}
	sourceRID := models.SourceRIDForConnection(conn.ID)
	binding := models.SourceBindingSnippet(sourceRID, settings.FriendlyName, settings.PythonIdentifier)
	return &models.SourceCodeImport{
		SourceID:                  conn.ID,
		SourceRID:                 sourceRID,
		SourceName:                conn.Name,
		ConnectorType:             conn.ConnectorType,
		Enabled:                   settings.Enabled,
		FriendlyName:              settings.FriendlyName,
		PythonIdentifier:          settings.PythonIdentifier,
		GeneratedBinding:          binding,
		CodeRepositories:          settings.CodeRepositories,
		ExportControls:            settings.ExportControls,
		ExternalTransformPatterns: models.ExternalTransformPatternsForSource(sourceRID, settings.FriendlyName, settings.PythonIdentifier, settings.ExportControls),
		ComputeModuleAlternatives: models.ComputeModuleAlternativesForSource(sourceRID, settings.FriendlyName, settings.PythonIdentifier),
		BuildStartResolution:      resolution,
		Warnings:                  resolution.Warnings,
		CreatedAt:                 settings.CreatedAt,
		UpdatedAt:                 settings.UpdatedAt,
	}, nil
}

func (r *Repo) buildSourceCodeImportResolution(ctx context.Context, conn *models.Connection, ownerID uuid.UUID, settings sourceCodeImportSettings, body *models.ResolveSourceCodeImportBuildRequest) (models.SourceCodeImportBuildResolution, error) {
	credentials, err := r.ListCredentials(ctx, conn.ID, ownerID)
	if err != nil {
		return models.SourceCodeImportBuildResolution{}, err
	}
	policies, err := r.ListSourcePolicies(ctx, conn.ID, ownerID)
	if err != nil {
		return models.SourceCodeImportBuildResolution{}, err
	}
	credentialBindings := make([]models.SourceCredentialBinding, 0, len(credentials))
	for _, credential := range credentials {
		credentialBindings = append(credentialBindings, models.SourceCredentialBinding{
			CredentialID: credential.ID,
			Kind:         credential.Kind,
			Fingerprint:  credential.Fingerprint,
			CreatedAt:    credential.CreatedAt,
		})
	}
	egressBindings := make([]models.SourceEgressPolicyBinding, 0, len(policies))
	for _, policy := range policies {
		egressBindings = append(egressBindings, models.SourceEgressPolicyBinding{PolicyID: policy.PolicyID, Kind: policy.Kind})
	}
	cfgHash := sha256.Sum256(conn.Config)
	resolvedAt := time.Now().UTC()
	sourceRID := models.SourceRIDForConnection(conn.ID)
	usesFoundryInputs := false
	foundryInputs := []models.SourceCodeImportFoundryInput{}
	if body != nil {
		foundryInputs = body.FoundryInputs
		if body.UsesFoundryInputs != nil {
			usesFoundryInputs = *body.UsesFoundryInputs
		}
	}
	exportPolicyDecision := models.ResolveSourceCodeImportExportPolicy(settings.ExportControls, usesFoundryInputs, foundryInputs)
	resolution := models.SourceCodeImportBuildResolution{
		SourceID:              conn.ID,
		SourceRID:             sourceRID,
		SourceName:            conn.Name,
		ConnectorType:         conn.ConnectorType,
		PythonIdentifier:      settings.PythonIdentifier,
		FriendlyName:          settings.FriendlyName,
		ResolvedAt:            resolvedAt,
		SourceUpdatedAt:       conn.UpdatedAt,
		ConfigHash:            fmt.Sprintf("sha256:%x", cfgHash[:]),
		CredentialBindings:    credentialBindings,
		EgressPolicyBindings:  egressBindings,
		ExportControls:        settings.ExportControls,
		ExportPolicyDecision:  exportPolicyDecision,
		UsesLiveConfiguration: true,
		NoCodeChangeRequired:  true,
		GeneratedBinding:      models.SourceBindingSnippet(sourceRID, settings.FriendlyName, settings.PythonIdentifier),
		Warnings:              models.SourceCodeImportWarnings(settings.Enabled, credentialBindings, egressBindings, settings.ExportControls, exportPolicyDecision),
	}
	if body != nil {
		resolution.RepositoryRID = cleanStringPtr(body.RepositoryRID)
		resolution.BuildRID = cleanStringPtr(body.BuildRID)
		resolution.Branch = cleanStringPtr(body.Branch)
	}
	return resolution, nil
}

const syncJobSelect = `SELECT d.id, d.source_id, COALESCE(d.capability_type, 'batch_sync'), COALESCE(d.output_kind, 'dataset'),
	d.output_dataset_id, d.output_stream_id, d.output_media_set_id, d.source_selector, d.source_path, d.source_table,
	d.source_topic, COALESCE(d.schema_json, '[]'::jsonb), d.write_mode, d.transaction_mode, d.build_integration,
	d.dataset_transaction_type, d.file_sync, d.table_sync, d.cdc_sync, d.file_glob, d.schedule_cron, d.created_at
	FROM batch_sync_defs d JOIN connections c ON c.id = d.source_id`

func (r *Repo) ListSyncJobs(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.SyncJob, error) {
	rows, err := r.Pool.Query(ctx, syncJobSelect+` WHERE d.source_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+` ORDER BY d.created_at DESC`,
		sourceID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.SyncJob, 0)
	for rows.Next() {
		v, err := scanSyncJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetSyncJob(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.SyncJob, error) {
	row := r.Pool.QueryRow(ctx, syncJobSelect+` WHERE d.id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4"),
		id, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	v, err := scanSyncJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func syncStringValue(ptr *string, fallback string) string {
	if ptr == nil || strings.TrimSpace(*ptr) == "" {
		return fallback
	}
	return strings.TrimSpace(*ptr)
}

func nullRaw(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

func (r *Repo) CreateSyncJob(ctx context.Context, body *models.CreateSyncJobRequest, ownerID uuid.UUID) (*models.SyncJob, error) {
	cdcSync, err := json.Marshal(body.CdcSync)
	if err != nil {
		return nil, err
	}
	if body.CdcSync == nil {
		cdcSync = nil
	}
	schema := body.Schema
	if len(schema) == 0 {
		schema = []byte(`[]`)
	}
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO batch_sync_defs (id, source_id, capability_type, output_kind, output_dataset_id,
		     output_stream_id, output_media_set_id, source_selector, source_path, source_table, source_topic,
		     schema_json, write_mode, transaction_mode, build_integration, dataset_transaction_type,
		     file_sync, table_sync, cdc_sync, file_glob, schedule_cron)
		 SELECT $1, c.id, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15, $16, $17::jsonb, $18::jsonb, $19::jsonb, $20, $21
			   FROM connections c WHERE c.id = $2 AND `+sourceAccessSQL("c", "$22", "$23", "$24")+`
			 RETURNING id, source_id, capability_type, output_kind, output_dataset_id, output_stream_id,
			           output_media_set_id, source_selector, source_path, source_table, source_topic, schema_json,
			           write_mode, transaction_mode, build_integration, dataset_transaction_type,
			           file_sync, table_sync, cdc_sync, file_glob, schedule_cron, created_at`,
		uuid.New(), body.SourceID, syncStringValue(body.CapabilityType, "batch_sync"), syncStringValue(body.OutputKind, "dataset"),
		body.OutputDatasetID, body.OutputStreamID, body.OutputMediaSetID, body.SourceSelector, body.SourcePath,
		body.SourceTable, body.SourceTopic, schema, body.WriteMode, body.TransactionMode, body.BuildIntegration,
		body.DatasetTransactionType, nullRaw(body.FileSync), nullRaw(body.TableSync), cdcSync, body.FileGlob, body.ScheduleCron,
		ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleSyncCreate),
	)
	v, err := scanSyncJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) UpdateSyncJob(ctx context.Context, id uuid.UUID, body *models.UpdateSyncJobRequest, ownerID uuid.UUID) (*models.SyncJob, error) {
	current, err := r.GetSyncJob(ctx, id, ownerID)
	if err != nil || current == nil {
		return current, err
	}
	output := current.OutputDatasetID
	if body.OutputDatasetID != nil {
		output = body.OutputDatasetID
	}
	outputStream := current.OutputStreamID
	if body.OutputStreamID != nil {
		outputStream = body.OutputStreamID
	}
	outputMedia := current.OutputMediaSetID
	if body.OutputMediaSetID != nil {
		outputMedia = body.OutputMediaSetID
	}
	sourceSelector := current.SourceSelector
	if body.SourceSelector != nil {
		sourceSelector = body.SourceSelector
	}
	sourcePath := current.SourcePath
	if body.SourcePath != nil {
		sourcePath = body.SourcePath
	}
	sourceTable := current.SourceTable
	if body.SourceTable != nil {
		sourceTable = body.SourceTable
	}
	sourceTopic := current.SourceTopic
	if body.SourceTopic != nil {
		sourceTopic = body.SourceTopic
	}
	schema := current.Schema
	if len(body.Schema) > 0 {
		schema = body.Schema
	}
	writeMode := current.WriteMode
	if body.WriteMode != nil {
		writeMode = body.WriteMode
	}
	transactionMode := current.TransactionMode
	if body.TransactionMode != nil {
		transactionMode = body.TransactionMode
	}
	buildIntegration := current.BuildIntegration
	if body.BuildIntegration != nil {
		buildIntegration = body.BuildIntegration
	}
	datasetTransactionType := current.DatasetTransactionType
	if body.DatasetTransactionType != nil {
		datasetTransactionType = body.DatasetTransactionType
	}
	fileSync := current.FileSync
	if len(body.FileSync) > 0 {
		fileSync = body.FileSync
	}
	tableSync := current.TableSync
	if len(body.TableSync) > 0 {
		tableSync = body.TableSync
	}
	cdcSync := current.CdcSync
	if body.CdcSync != nil {
		cdcSync = body.CdcSync
	}
	cdcSyncJSON, err := json.Marshal(cdcSync)
	if err != nil {
		return nil, err
	}
	if cdcSync == nil {
		cdcSyncJSON = nil
	}
	fileGlob := current.FileGlob
	if body.FileGlob != nil {
		fileGlob = body.FileGlob
	}
	schedule := current.ScheduleCron
	if body.ScheduleCron != nil {
		schedule = body.ScheduleCron
	}
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE batch_sync_defs d SET output_dataset_id = $2, output_stream_id = $3, output_media_set_id = $4,
		     source_selector = $5, source_path = $6, source_table = $7, source_topic = $8, schema_json = $9::jsonb,
		     write_mode = $10, transaction_mode = $11, build_integration = $12, dataset_transaction_type = $13,
		     file_sync = $14::jsonb, table_sync = $15::jsonb, cdc_sync = $16::jsonb, file_glob = $17, schedule_cron = $18
			  FROM connections c WHERE d.source_id = c.id AND d.id = $1 AND `+sourceAccessSQL("c", "$19", "$20", "$21")+`
			  RETURNING d.id, d.source_id, d.capability_type, d.output_kind, d.output_dataset_id, d.output_stream_id,
		            d.output_media_set_id, d.source_selector, d.source_path, d.source_table, d.source_topic, d.schema_json,
		            d.write_mode, d.transaction_mode, d.build_integration, d.dataset_transaction_type,
		            d.file_sync, d.table_sync, d.cdc_sync, d.file_glob, d.schedule_cron, d.created_at`,
		id, output, outputStream, outputMedia, sourceSelector, sourcePath, sourceTable, sourceTopic, schema,
		writeMode, transactionMode, buildIntegration, datasetTransactionType, nullRaw(fileSync), nullRaw(tableSync),
		cdcSyncJSON, fileGlob, schedule, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleEdit),
	)
	return scanSyncJob(row)
}

func (r *Repo) RunSyncJob(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.SyncRun, error) {
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO sync_runs (id, sync_def_id, status)
			 SELECT $1, d.id, 'running' FROM batch_sync_defs d JOIN connections c ON c.id = d.source_id
			 WHERE d.id = $2 AND `+sourceAccessSQL("c", "$3", "$4", "$5")+`
			 RETURNING id, sync_def_id, status, started_at, finished_at, bytes_written, files_written, error,
			           ingest_job_id, dataset_version_id, content_hash`,
		uuid.New(), id, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleUse),
	)
	v, err := scanSyncRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CompleteSyncRun(ctx context.Context, runID uuid.UUID, ownerID uuid.UUID, status string, bytesWritten int64, filesWritten int64, errMsg *string, ingestJobID *string, datasetVersionID *uuid.UUID, contentHash *string) (*models.SyncRun, error) {
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE sync_runs r
		    SET status = $2,
		        finished_at = NOW(),
		        bytes_written = $3,
		        files_written = $4,
		        error = $5,
		        ingest_job_id = $6,
		        dataset_version_id = $7,
		        content_hash = $8
		   FROM batch_sync_defs d
		   JOIN connections c ON c.id = d.source_id
			  WHERE r.sync_def_id = d.id AND r.id = $1 AND `+sourceAccessSQL("c", "$9", "$10", "$11")+`
			  RETURNING r.id, r.sync_def_id, r.status, r.started_at, r.finished_at, r.bytes_written,
		            r.files_written, r.error, r.ingest_job_id, r.dataset_version_id, r.content_hash`,
		runID, status, bytesWritten, filesWritten, errMsg, ingestJobID, datasetVersionID, contentHash,
		ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleUse),
	)
	v, err := scanSyncRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) PreviousDatasetVersionForHash(ctx context.Context, syncDefID uuid.UUID, contentHash string) (*uuid.UUID, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT dataset_version_id
		   FROM sync_runs
		  WHERE sync_def_id = $1 AND content_hash = $2 AND dataset_version_id IS NOT NULL
		  ORDER BY started_at DESC
		  LIMIT 1`, syncDefID, contentHash)
	var id uuid.UUID
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &id, nil
}

func (r *Repo) RecordDatasetVersionOnRun(ctx context.Context, runID uuid.UUID, datasetVersionID uuid.UUID, contentHash string) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE sync_runs SET dataset_version_id = $2, content_hash = $3 WHERE id = $1`,
		runID, datasetVersionID, contentHash)
	return err
}

func (r *Repo) ListSyncRuns(ctx context.Context, syncID uuid.UUID, ownerID uuid.UUID) ([]models.SyncRun, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT r.id, r.sync_def_id, r.status, r.started_at, r.finished_at, r.bytes_written,
		        r.files_written, r.error, r.ingest_job_id, r.dataset_version_id, r.content_hash
		 FROM sync_runs r
		 JOIN batch_sync_defs d ON d.id = r.sync_def_id
		 JOIN connections c ON c.id = d.source_id
		 WHERE r.sync_def_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+`
		 ORDER BY r.started_at DESC LIMIT 500`, syncID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.SyncRun, 0)
	for rows.Next() {
		v, err := scanSyncRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func scanSyncJob(r rowLikeT) (*models.SyncJob, error) {
	v := &models.SyncJob{}
	var cdcSync []byte
	if err := r.Scan(&v.ID, &v.SourceID, &v.CapabilityType, &v.OutputKind, &v.OutputDatasetID, &v.OutputStreamID,
		&v.OutputMediaSetID, &v.SourceSelector, &v.SourcePath, &v.SourceTable, &v.SourceTopic, &v.Schema,
		&v.WriteMode, &v.TransactionMode, &v.BuildIntegration, &v.DatasetTransactionType, &v.FileSync,
		&v.TableSync, &cdcSync, &v.FileGlob, &v.ScheduleCron, &v.CreatedAt); err != nil {
		return nil, err
	}
	if len(v.Schema) == 0 {
		v.Schema = []byte(`[]`)
	}
	if len(cdcSync) > 0 && string(cdcSync) != "null" {
		var settings models.CdcSyncSettings
		if err := json.Unmarshal(cdcSync, &settings); err != nil {
			return nil, err
		}
		v.CdcSync = &settings
	}
	return v, nil
}

func scanSyncRun(r rowLikeT) (*models.SyncRun, error) {
	v := &models.SyncRun{}
	if err := r.Scan(&v.ID, &v.SyncDefID, &v.Status, &v.StartedAt, &v.FinishedAt, &v.BytesWritten,
		&v.FilesWritten, &v.Error, &v.IngestJobID, &v.DatasetVersionID, &v.ContentHash); err != nil {
		return nil, err
	}
	return v, nil
}

const dataExportSelect = `SELECT e.id, e.source_id, e.name, e.export_type, e.export_mode,
	e.input_dataset_id, e.input_dataset_rid, e.input_stream_id, e.destination_path,
	e.destination_table, e.destination_topic, e.schedule_cron, e.start_behavior,
	e.stop_behavior, e.export_controls, e.config, e.file_export, e.table_export, e.streaming_export, e.status, e.health, e.history,
	e.last_run_at, e.created_by, e.created_at, e.updated_at
	FROM data_exports e JOIN connections c ON c.id = e.source_id`

func (r *Repo) ListDataExports(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.DataExport, error) {
	rows, err := r.Pool.Query(ctx, dataExportSelect+` WHERE e.source_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+` ORDER BY e.created_at DESC`,
		sourceID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.DataExport, 0)
	for rows.Next() {
		v, err := scanDataExport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error) {
	row := r.Pool.QueryRow(ctx, dataExportSelect+` WHERE e.id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4"),
		id, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	v, err := scanDataExport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func cleanStringPtr(ptr *string) *string {
	if ptr == nil {
		return nil
	}
	clean := strings.TrimSpace(*ptr)
	if clean == "" {
		return nil
	}
	return &clean
}

func exportHistory(action, status, message string, startedAt, finishedAt *time.Time) models.DataExportHistoryEntry {
	now := time.Now().UTC()
	var msg *string
	if strings.TrimSpace(message) != "" {
		clean := strings.TrimSpace(message)
		msg = &clean
	}
	return models.DataExportHistoryEntry{
		ID:         uuid.New(),
		Action:     action,
		Status:     status,
		Message:    msg,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		CreatedAt:  now,
	}
}

func decorateDataExportBuildHistory(current *models.DataExport, entry *models.DataExportHistoryEntry, triggeredAt time.Time) {
	if current == nil || entry == nil {
		return
	}
	buildID := models.NewDataExportBuildID()
	reportURL := models.DataExportBuildReportURL(buildID)
	entry.BuildID = &buildID
	entry.BuildReportURL = &reportURL
	entry.RetryAttempts = models.DataExportRetryAttempts(current.Config)
	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}
	entry.Metadata["build_id"] = buildID
	entry.Metadata["build_report_url"] = reportURL
	entry.Metadata["retry_attempts"] = entry.RetryAttempts
	entry.Metadata["build_system"] = "data-integration-build-schedules"
	lastTriggeredAt := triggeredAt
	schedule := models.DataExportScheduleFor(current.ID, current.Name, current.ExportType, current.ScheduleCron, &lastTriggeredAt)
	if schedule != nil {
		entry.ScheduleTriggered = true
		entry.Metadata["triggered_by"] = "schedule"
		entry.Metadata["schedule"] = schedule
		entry.Metadata["schedule_rid"] = schedule.RID
		return
	}
	entry.Metadata["triggered_by"] = "manual"
}

func (r *Repo) CreateDataExport(ctx context.Context, body *models.CreateDataExportRequest, ownerID uuid.UUID) (*models.DataExport, error) {
	models.NormalizeCreateDataExportRequest(body)
	controls, err := json.Marshal(body.ExportControls)
	if err != nil {
		return nil, err
	}
	fileExportJSON := []byte(`{}`)
	if body.FileExport != nil {
		fileExportJSON, err = json.Marshal(body.FileExport)
		if err != nil {
			return nil, err
		}
	}
	tableExportJSON := []byte(`{}`)
	if body.TableExport != nil {
		tableExportJSON, err = json.Marshal(body.TableExport)
		if err != nil {
			return nil, err
		}
	}
	streamingExportJSON := []byte(`{}`)
	if body.StreamingExport != nil {
		streamingExportJSON, err = json.Marshal(body.StreamingExport)
		if err != nil {
			return nil, err
		}
	}
	health := models.DefaultDataExportHealth()
	healthJSON, err := json.Marshal(health)
	if err != nil {
		return nil, err
	}
	status := models.DataExportStatusDraft
	if body.ScheduleCron != nil {
		status = models.DataExportStatusScheduled
	}
	historyJSON, err := json.Marshal([]models.DataExportHistoryEntry{
		exportHistory("created", string(status), "Export resource created", nil, nil),
	})
	if err != nil {
		return nil, err
	}
	name := body.Name
	if name == "" {
		switch body.ExportType {
		case models.DataExportTypeFile:
			name = "File export"
		case models.DataExportTypeTable:
			name = "Table export"
		case models.DataExportTypeStreaming:
			name = "Streaming export"
		default:
			name = "Export"
		}
	}
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO data_exports (id, source_id, name, export_type, export_mode,
		     input_dataset_id, input_dataset_rid, input_stream_id, destination_path,
		     destination_table, destination_topic, schedule_cron, start_behavior, stop_behavior,
		     export_controls, config, file_export, table_export, streaming_export, status, health, history, created_by)
		 SELECT $1, c.id, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
		        $13, $14, $15::jsonb, $16::jsonb, $17::jsonb, $18::jsonb, $19::jsonb, $20, $21::jsonb, $22::jsonb, $23
		   FROM connections c WHERE c.id = $2 AND `+sourceAccessSQL("c", "$24", "$25", "$26")+`
		 RETURNING id, source_id, name, export_type, export_mode, input_dataset_id,
		           input_dataset_rid, input_stream_id, destination_path, destination_table,
		           destination_topic, schedule_cron, start_behavior, stop_behavior,
		           export_controls, config, file_export, table_export, streaming_export, status, health, history, last_run_at,
		           created_by, created_at, updated_at`,
		uuid.New(), body.SourceID, name, string(body.ExportType), string(body.ExportMode),
		body.InputDatasetID, cleanStringPtr(body.InputDatasetRID), cleanStringPtr(body.InputStreamID),
		cleanStringPtr(body.DestinationPath), cleanStringPtr(body.DestinationTable),
		cleanStringPtr(body.DestinationTopic), cleanStringPtr(body.ScheduleCron),
		body.StartBehavior, body.StopBehavior, controls, body.Config, fileExportJSON, tableExportJSON,
		streamingExportJSON, string(status), healthJSON, historyJSON, ownerID, ownerID,
		ownerID.String(), sourceAccessRoleNames(models.SourceRoleExportCreate),
	)
	v, err := scanDataExport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) UpdateDataExport(ctx context.Context, id uuid.UUID, body *models.UpdateDataExportRequest, ownerID uuid.UUID) (*models.DataExport, error) {
	current, err := r.GetDataExport(ctx, id, ownerID)
	if err != nil || current == nil {
		return current, err
	}
	name := current.Name
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
	}
	mode := current.ExportMode
	if body.ExportMode != nil {
		mode = *body.ExportMode
	}
	inputDatasetID := current.InputDatasetID
	if body.InputDatasetID != nil {
		inputDatasetID = body.InputDatasetID
	}
	inputDatasetRID := current.InputDatasetRID
	if body.InputDatasetRID != nil {
		inputDatasetRID = cleanStringPtr(body.InputDatasetRID)
	}
	inputStreamID := current.InputStreamID
	if body.InputStreamID != nil {
		inputStreamID = cleanStringPtr(body.InputStreamID)
	}
	destinationPath := current.DestinationPath
	if body.DestinationPath != nil {
		destinationPath = cleanStringPtr(body.DestinationPath)
	}
	destinationTable := current.DestinationTable
	if body.DestinationTable != nil {
		destinationTable = cleanStringPtr(body.DestinationTable)
	}
	destinationTopic := current.DestinationTopic
	if body.DestinationTopic != nil {
		destinationTopic = cleanStringPtr(body.DestinationTopic)
	}
	schedule := current.ScheduleCron
	if body.ScheduleCron != nil {
		schedule = cleanStringPtr(body.ScheduleCron)
	}
	startBehavior := current.StartBehavior
	if body.StartBehavior != nil {
		startBehavior = strings.TrimSpace(*body.StartBehavior)
	}
	stopBehavior := current.StopBehavior
	if body.StopBehavior != nil {
		stopBehavior = strings.TrimSpace(*body.StopBehavior)
	}
	controls := current.ExportControls
	if body.ExportControls != nil {
		controls = *body.ExportControls
	}
	config := current.Config
	if len(body.Config) > 0 && string(body.Config) != "null" {
		config = body.Config
	}
	fileExport := current.FileExport
	if body.FileExport != nil {
		normalized := *body.FileExport
		models.NormalizeFileExportSettings(&normalized, syncStringValue(destinationPath, ""), mode)
		fileExport = &normalized
	}
	tableExport := current.TableExport
	if body.TableExport != nil {
		normalized := *body.TableExport
		models.NormalizeTableExportSettings(&normalized, mode)
		tableExport = &normalized
	} else if tableExport != nil {
		normalized := *tableExport
		models.NormalizeTableExportSettings(&normalized, mode)
		tableExport = &normalized
	}
	streamingExport := current.StreamingExport
	if body.StreamingExport != nil {
		normalized := *body.StreamingExport
		models.NormalizeStreamingExportSettings(&normalized, schedule != nil)
		streamingExport = &normalized
	} else if streamingExport != nil {
		normalized := *streamingExport
		models.NormalizeStreamingExportSettings(&normalized, schedule != nil)
		streamingExport = &normalized
	}
	controlsJSON, err := json.Marshal(controls)
	if err != nil {
		return nil, err
	}
	fileExportJSON := []byte(`{}`)
	if fileExport != nil {
		fileExportJSON, err = json.Marshal(fileExport)
		if err != nil {
			return nil, err
		}
	}
	tableExportJSON := []byte(`{}`)
	if tableExport != nil {
		tableExportJSON, err = json.Marshal(tableExport)
		if err != nil {
			return nil, err
		}
	}
	streamingExportJSON := []byte(`{}`)
	if streamingExport != nil {
		streamingExportJSON, err = json.Marshal(streamingExport)
		if err != nil {
			return nil, err
		}
	}
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE data_exports e SET name = $2, export_mode = $3,
		     input_dataset_id = $4, input_dataset_rid = $5, input_stream_id = $6,
		     destination_path = $7, destination_table = $8, destination_topic = $9,
		     schedule_cron = $10, start_behavior = $11, stop_behavior = $12,
		     export_controls = $13::jsonb, config = $14::jsonb, file_export = $15::jsonb,
		     table_export = $16::jsonb, streaming_export = $17::jsonb, updated_at = NOW()
		  FROM connections c WHERE e.source_id = c.id AND e.id = $1 AND `+sourceAccessSQL("c", "$18", "$19", "$20")+`
		  RETURNING e.id, e.source_id, e.name, e.export_type, e.export_mode, e.input_dataset_id,
		            e.input_dataset_rid, e.input_stream_id, e.destination_path, e.destination_table,
		            e.destination_topic, e.schedule_cron, e.start_behavior, e.stop_behavior,
		            e.export_controls, e.config, e.file_export, e.table_export, e.streaming_export, e.status, e.health, e.history, e.last_run_at,
		            e.created_by, e.created_at, e.updated_at`,
		id, name, string(mode), inputDatasetID, inputDatasetRID, inputStreamID, destinationPath,
		destinationTable, destinationTopic, schedule, startBehavior, stopBehavior, controlsJSON,
		config, fileExportJSON, tableExportJSON, streamingExportJSON,
		ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleEdit),
	)
	v, err := scanDataExport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) RunDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error) {
	current, err := r.GetDataExport(ctx, id, ownerID)
	if err != nil || current == nil {
		return current, err
	}
	if current.ExportType == models.DataExportTypeFile {
		return r.runFileDataExport(ctx, current, ownerID)
	}
	if current.ExportType == models.DataExportTypeTable {
		return r.runTableDataExport(ctx, current, ownerID)
	}
	now := time.Now().UTC()
	health := models.DataExportHealth{State: models.DataExportHealthHealthy, LastCheckedAt: &now}
	return r.transitionDataExport(ctx, id, ownerID, models.DataExportStatusSucceeded, health, exportHistory("run", string(models.DataExportStatusSucceeded), "Export run completed", &now, &now), &now)
}

func (r *Repo) runFileDataExport(ctx context.Context, current *models.DataExport, ownerID uuid.UUID) (*models.DataExport, error) {
	now := time.Now().UTC()
	settings := models.DefaultFileExportSettings(syncStringValue(current.DestinationPath, ""), current.ExportMode)
	if current.FileExport != nil {
		settings = *current.FileExport
		models.NormalizeFileExportSettings(&settings, syncStringValue(current.DestinationPath, ""), current.ExportMode)
	}
	plan := models.BuildFileExportRunPlan(settings, syncStringValue(current.DestinationPath, ""), now)
	settings.LastSuccessfulAt = &now
	if plan.LastExportedTransactionID != nil {
		tx := *plan.LastExportedTransactionID
		settings.LastSuccessfulTransactionID = &tx
	}
	settings.FullReexportRequested = false
	models.NormalizeFileExportSettings(&settings, syncStringValue(current.DestinationPath, ""), current.ExportMode)
	message := fmt.Sprintf("File export completed: %d file(s) written, %d skipped, %d bytes", plan.FilesWritten, plan.FilesSkipped, plan.BytesWritten)
	health := models.DataExportHealth{State: models.DataExportHealthHealthy, Message: &message, LastCheckedAt: &now}
	entry := exportHistory("run", string(models.DataExportStatusSucceeded), message, &now, &now)
	entry.FilesWritten = plan.FilesWritten
	entry.FilesSkipped = plan.FilesSkipped
	entry.BytesWritten = plan.BytesWritten
	entry.HighWatermarkTransactionID = plan.LastExportedTransactionID
	entry.FullReexport = plan.FullReexport
	entry.Metadata = map[string]any{
		"export_type":          "file",
		"incremental_policy":   plan.IncrementalPolicy,
		"overwrite_behavior":   plan.OverwriteBehavior,
		"destination_path":     plan.DestinationPath,
		"files_considered":     plan.FilesConsidered,
		"destination_guidance": plan.DestinationSubfolderAdvice,
	}
	if plan.DestinationSubfolder != nil {
		entry.Metadata["destination_subfolder"] = *plan.DestinationSubfolder
	}
	decorateDataExportBuildHistory(current, &entry, now)
	history := append([]models.DataExportHistoryEntry{entry}, current.History...)
	healthJSON, err := json.Marshal(health)
	if err != nil {
		return nil, err
	}
	historyJSON, err := json.Marshal(history)
	if err != nil {
		return nil, err
	}
	fileExportJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE data_exports e SET status = $2, health = $3::jsonb, history = $4::jsonb,
		     file_export = $5::jsonb, last_run_at = $6, updated_at = NOW()
		  FROM connections c WHERE e.source_id = c.id AND e.id = $1 AND `+sourceAccessSQL("c", "$7", "$8", "$9")+`
		  RETURNING e.id, e.source_id, e.name, e.export_type, e.export_mode, e.input_dataset_id,
		            e.input_dataset_rid, e.input_stream_id, e.destination_path, e.destination_table,
		            e.destination_topic, e.schedule_cron, e.start_behavior, e.stop_behavior,
		            e.export_controls, e.config, e.file_export, e.table_export, e.streaming_export, e.status, e.health, e.history, e.last_run_at,
		            e.created_by, e.created_at, e.updated_at`,
		current.ID, string(models.DataExportStatusSucceeded), healthJSON, historyJSON, fileExportJSON, now,
		ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleUse),
	)
	v, err := scanDataExport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) runTableDataExport(ctx context.Context, current *models.DataExport, ownerID uuid.UUID) (*models.DataExport, error) {
	now := time.Now().UTC()
	settings := models.DefaultTableExportSettings(current.ExportMode)
	if current.TableExport != nil {
		settings = *current.TableExport
	}
	models.NormalizeTableExportSettings(&settings, current.ExportMode)
	plan := models.BuildTableExportRunPlan(settings, current.ExportMode, now)
	hasErrors := false
	for _, issue := range plan.ValidationIssues {
		if issue.Severity == "error" {
			hasErrors = true
			break
		}
	}
	status := models.DataExportStatusSucceeded
	healthState := models.DataExportHealthHealthy
	message := fmt.Sprintf("Table export completed: %d row(s) written to %s using %s", plan.RowsWritten, syncStringValue(current.DestinationTable, ""), plan.ResolutionStrategy)
	if hasErrors {
		status = models.DataExportStatusFailed
		healthState = models.DataExportHealthError
		message = "Table export blocked by validation issues"
	} else {
		settings.LastSuccessfulAt = &now
	}
	models.NormalizeTableExportSettings(&settings, current.ExportMode)
	health := models.DataExportHealth{State: healthState, Message: &message, LastCheckedAt: &now}
	entry := exportHistory("run", string(status), message, &now, &now)
	entry.RowsWritten = plan.RowsWritten
	entry.TruncatePerformed = plan.TruncatePerformed && !hasErrors
	if hasErrors {
		entry.ErrorMessage = &message
	}
	entry.Metadata = map[string]any{
		"export_type":              "table",
		"export_mode":              string(plan.ExportMode),
		"resolution_strategy":      plan.ResolutionStrategy,
		"destination_table":        syncStringValue(current.DestinationTable, ""),
		"input_parquet_backed":     plan.InputParquetBacked,
		"destination_table_exists": plan.DestinationTableExists,
		"exact_column_match":       plan.ExactColumnMatch,
		"truncate_required":        plan.TruncateRequired,
		"validation_issues":        plan.ValidationIssues,
	}
	decorateDataExportBuildHistory(current, &entry, now)
	history := append([]models.DataExportHistoryEntry{entry}, current.History...)
	healthJSON, err := json.Marshal(health)
	if err != nil {
		return nil, err
	}
	historyJSON, err := json.Marshal(history)
	if err != nil {
		return nil, err
	}
	tableExportJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE data_exports e SET status = $2, health = $3::jsonb, history = $4::jsonb,
		     table_export = $5::jsonb, last_run_at = $6, updated_at = NOW()
		  FROM connections c WHERE e.source_id = c.id AND e.id = $1 AND `+sourceAccessSQL("c", "$7", "$8", "$9")+`
		  RETURNING e.id, e.source_id, e.name, e.export_type, e.export_mode, e.input_dataset_id,
		            e.input_dataset_rid, e.input_stream_id, e.destination_path, e.destination_table,
		            e.destination_topic, e.schedule_cron, e.start_behavior, e.stop_behavior,
		            e.export_controls, e.config, e.file_export, e.table_export, e.streaming_export, e.status, e.health, e.history, e.last_run_at,
		            e.created_by, e.created_at, e.updated_at`,
		current.ID, string(status), healthJSON, historyJSON, tableExportJSON, now,
		ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleUse),
	)
	v, err := scanDataExport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) StartDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error) {
	current, err := r.GetDataExport(ctx, id, ownerID)
	if err != nil || current == nil {
		return current, err
	}
	if current.ExportType == models.DataExportTypeStreaming {
		return r.startStreamingDataExport(ctx, current, ownerID, false)
	}
	now := time.Now().UTC()
	health := models.DataExportHealth{State: models.DataExportHealthRunning, LastCheckedAt: &now}
	return r.transitionDataExport(ctx, id, ownerID, models.DataExportStatusRunning, health, exportHistory("started", string(models.DataExportStatusRunning), "Streaming export started", &now, nil), &now)
}

func (r *Repo) StopDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error) {
	current, err := r.GetDataExport(ctx, id, ownerID)
	if err != nil || current == nil {
		return current, err
	}
	if current.ExportType == models.DataExportTypeStreaming {
		return r.stopStreamingDataExport(ctx, current, ownerID)
	}
	now := time.Now().UTC()
	health := models.DataExportHealth{State: models.DataExportHealthHealthy, LastCheckedAt: &now}
	return r.transitionDataExport(ctx, id, ownerID, models.DataExportStatusStopped, health, exportHistory("stopped", string(models.DataExportStatusStopped), "Streaming export stopped", nil, &now), nil)
}

func (r *Repo) startStreamingDataExport(ctx context.Context, current *models.DataExport, ownerID uuid.UUID, scheduleTriggered bool) (*models.DataExport, error) {
	now := time.Now().UTC()
	settings := models.DefaultStreamingExportSettings(current.ScheduleCron != nil)
	if current.StreamingExport != nil {
		settings = *current.StreamingExport
	}
	models.NormalizeStreamingExportSettings(&settings, current.ScheduleCron != nil)
	plan := models.BuildStreamingExportStartPlan(settings, scheduleTriggered, now)
	settings.LastStartedAt = &now
	models.NormalizeStreamingExportSettings(&settings, current.ScheduleCron != nil)
	message := "Streaming export started"
	if plan.EffectiveStartOffset != nil {
		message = fmt.Sprintf("Streaming export started from offset %s", *plan.EffectiveStartOffset)
	}
	health := models.DataExportHealth{State: models.DataExportHealthRunning, Message: &message, LastCheckedAt: &now}
	entry := exportHistory("started", string(models.DataExportStatusRunning), message, &now, nil)
	entry.LastExportedOffset = plan.EffectiveStartOffset
	entry.ReplayBehavior = plan.ReplayBehavior
	entry.ScheduleTriggered = scheduleTriggered
	entry.Metadata = map[string]any{
		"export_type":                  "streaming",
		"destination_topic":            syncStringValue(current.DestinationTopic, ""),
		"input_stream_id":              syncStringValue(current.InputStreamID, ""),
		"start_offset":                 plan.StartOffset,
		"restart_from_previous_offset": plan.RestartFromPreviousOffset,
		"schedule_restart_enabled":     plan.ScheduleRestartEnabled,
		"duplicate_risk":               plan.DuplicateRisk,
		"drop_risk":                    plan.DropRisk,
		"warnings":                     plan.Warnings,
	}
	return r.updateStreamingExportState(ctx, current, ownerID, models.DataExportStatusRunning, health, entry, &settings, &now)
}

func (r *Repo) stopStreamingDataExport(ctx context.Context, current *models.DataExport, ownerID uuid.UUID) (*models.DataExport, error) {
	now := time.Now().UTC()
	settings := models.DefaultStreamingExportSettings(current.ScheduleCron != nil)
	if current.StreamingExport != nil {
		settings = *current.StreamingExport
	}
	models.NormalizeStreamingExportSettings(&settings, current.ScheduleCron != nil)
	records := int64(0)
	if settings.RecordsExportedEstimate != nil && *settings.RecordsExportedEstimate > 0 {
		records = *settings.RecordsExportedEstimate
	}
	nextOffset := models.AdvanceStreamingExportOffset(settings)
	settings.LastExportedOffset = nextOffset
	settings.LastStoppedAt = &now
	models.NormalizeStreamingExportSettings(&settings, current.ScheduleCron != nil)
	message := fmt.Sprintf("Streaming export stopped after exporting %d record(s)", records)
	health := models.DataExportHealth{State: models.DataExportHealthHealthy, Message: &message, LastCheckedAt: &now}
	entry := exportHistory("stopped", string(models.DataExportStatusStopped), message, nil, &now)
	entry.RecordsExported = records
	entry.LastExportedOffset = nextOffset
	entry.ReplayBehavior = settings.ReplayBehavior
	entry.Metadata = map[string]any{
		"export_type":                  "streaming",
		"destination_topic":            syncStringValue(current.DestinationTopic, ""),
		"input_stream_id":              syncStringValue(current.InputStreamID, ""),
		"restart_from_previous_offset": settings.RestartFromPreviousOffset,
		"schedule_restart_enabled":     settings.ScheduleRestartEnabled,
		"warnings":                     settings.Warnings,
	}
	return r.updateStreamingExportState(ctx, current, ownerID, models.DataExportStatusStopped, health, entry, &settings, nil)
}

func (r *Repo) updateStreamingExportState(ctx context.Context, current *models.DataExport, ownerID uuid.UUID, status models.DataExportStatus, health models.DataExportHealth, entry models.DataExportHistoryEntry, settings *models.StreamingExportSettings, lastRunAt *time.Time) (*models.DataExport, error) {
	history := append([]models.DataExportHistoryEntry{entry}, current.History...)
	healthJSON, err := json.Marshal(health)
	if err != nil {
		return nil, err
	}
	historyJSON, err := json.Marshal(history)
	if err != nil {
		return nil, err
	}
	streamingExportJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE data_exports e SET status = $2, health = $3::jsonb, history = $4::jsonb,
		     streaming_export = $5::jsonb, last_run_at = COALESCE($6::timestamptz, e.last_run_at), updated_at = NOW()
		  FROM connections c WHERE e.source_id = c.id AND e.id = $1 AND `+sourceAccessSQL("c", "$7", "$8", "$9")+`
		  RETURNING e.id, e.source_id, e.name, e.export_type, e.export_mode, e.input_dataset_id,
		            e.input_dataset_rid, e.input_stream_id, e.destination_path, e.destination_table,
		            e.destination_topic, e.schedule_cron, e.start_behavior, e.stop_behavior,
		            e.export_controls, e.config, e.file_export, e.table_export, e.streaming_export, e.status, e.health, e.history, e.last_run_at,
		            e.created_by, e.created_at, e.updated_at`,
		current.ID, string(status), healthJSON, historyJSON, streamingExportJSON, lastRunAt,
		ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleUse),
	)
	v, err := scanDataExport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) transitionDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID, status models.DataExportStatus, health models.DataExportHealth, entry models.DataExportHistoryEntry, lastRunAt *time.Time) (*models.DataExport, error) {
	current, err := r.GetDataExport(ctx, id, ownerID)
	if err != nil || current == nil {
		return current, err
	}
	history := append([]models.DataExportHistoryEntry{}, current.History...)
	history = append([]models.DataExportHistoryEntry{entry}, history...)
	healthJSON, err := json.Marshal(health)
	if err != nil {
		return nil, err
	}
	historyJSON, err := json.Marshal(history)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE data_exports e SET status = $2, health = $3::jsonb, history = $4::jsonb,
		     last_run_at = COALESCE($5::timestamptz, e.last_run_at), updated_at = NOW()
		  FROM connections c WHERE e.source_id = c.id AND e.id = $1 AND `+sourceAccessSQL("c", "$6", "$7", "$8")+`
		  RETURNING e.id, e.source_id, e.name, e.export_type, e.export_mode, e.input_dataset_id,
		            e.input_dataset_rid, e.input_stream_id, e.destination_path, e.destination_table,
		            e.destination_topic, e.schedule_cron, e.start_behavior, e.stop_behavior,
		            e.export_controls, e.config, e.file_export, e.table_export, e.streaming_export, e.status, e.health, e.history, e.last_run_at,
		            e.created_by, e.created_at, e.updated_at`,
		id, string(status), healthJSON, historyJSON, lastRunAt,
		ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleUse),
	)
	v, err := scanDataExport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func scanDataExport(r rowLikeT) (*models.DataExport, error) {
	v := &models.DataExport{}
	var exportType, mode, status string
	var controlsJSON, fileExportJSON, tableExportJSON, streamingExportJSON, healthJSON, historyJSON []byte
	if err := r.Scan(&v.ID, &v.SourceID, &v.Name, &exportType, &mode,
		&v.InputDatasetID, &v.InputDatasetRID, &v.InputStreamID, &v.DestinationPath,
		&v.DestinationTable, &v.DestinationTopic, &v.ScheduleCron, &v.StartBehavior,
		&v.StopBehavior, &controlsJSON, &v.Config, &fileExportJSON, &tableExportJSON, &streamingExportJSON, &status, &healthJSON, &historyJSON,
		&v.LastRunAt, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	v.ExportType = models.DataExportType(exportType)
	v.ExportMode = models.DataExportMode(mode)
	v.Status = models.DataExportStatus(status)
	if len(v.Config) == 0 {
		v.Config = []byte(`{}`)
	}
	if len(controlsJSON) == 0 || string(controlsJSON) == "null" {
		controlsJSON = []byte(`{}`)
	}
	if err := json.Unmarshal(controlsJSON, &v.ExportControls); err != nil {
		return nil, err
	}
	if len(fileExportJSON) > 0 && string(fileExportJSON) != "null" && string(fileExportJSON) != "{}" {
		var settings models.FileExportSettings
		if err := json.Unmarshal(fileExportJSON, &settings); err != nil {
			return nil, err
		}
		models.NormalizeFileExportSettings(&settings, syncStringValue(v.DestinationPath, ""), v.ExportMode)
		v.FileExport = &settings
	}
	if len(tableExportJSON) > 0 && string(tableExportJSON) != "null" && string(tableExportJSON) != "{}" {
		var settings models.TableExportSettings
		if err := json.Unmarshal(tableExportJSON, &settings); err != nil {
			return nil, err
		}
		models.NormalizeTableExportSettings(&settings, v.ExportMode)
		v.TableExport = &settings
	}
	if len(streamingExportJSON) > 0 && string(streamingExportJSON) != "null" && string(streamingExportJSON) != "{}" {
		var settings models.StreamingExportSettings
		if err := json.Unmarshal(streamingExportJSON, &settings); err != nil {
			return nil, err
		}
		models.NormalizeStreamingExportSettings(&settings, v.ScheduleCron != nil)
		v.StreamingExport = &settings
	}
	if len(healthJSON) == 0 || string(healthJSON) == "null" {
		v.Health = models.DefaultDataExportHealth()
	} else if err := json.Unmarshal(healthJSON, &v.Health); err != nil {
		return nil, err
	}
	if len(historyJSON) > 0 && string(historyJSON) != "null" {
		if err := json.Unmarshal(historyJSON, &v.History); err != nil {
			return nil, err
		}
	}
	if v.History == nil {
		v.History = []models.DataExportHistoryEntry{}
	}
	v.Schedule = models.DataExportScheduleFor(v.ID, v.Name, v.ExportType, v.ScheduleCron, v.LastRunAt)
	return v, nil
}

var validVirtualProviders = map[string]bool{
	"AMAZON_S3": true, "AZURE_ABFS": true, "BIGQUERY": true, "DATABRICKS": true,
	"FOUNDRY_ICEBERG": true, "GCS": true, "SNOWFLAKE": true,
}

var validVirtualTableTypes = map[string]bool{
	"TABLE": true, "VIEW": true, "MATERIALIZED_VIEW": true, "EXTERNAL_DELTA": true,
	"MANAGED_DELTA": true, "MANAGED_ICEBERG": true, "PARQUET_FILES": true,
	"AVRO_FILES": true, "CSV_FILES": true, "OTHER": true,
}

var bulkVirtualTableProviders = map[string]bool{
	"BIGQUERY": true, "DATABRICKS": true, "SNOWFLAKE": true,
}

var ErrConflict = errors.New("conflict")

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 23505")
}

const virtualTableSourceLinkColumns = `source_rid, provider, virtual_tables_enabled, code_imports_enabled, export_controls,
	auto_register_project_rid, auto_register_enabled, auto_register_interval_seconds,
	auto_register_tag_filters, auto_register_folder_mirror_kind, auto_register_table_tag_filters,
	iceberg_catalog_kind, iceberg_catalog_config, created_at, updated_at`

func (r *Repo) EnableVirtualTableSource(ctx context.Context, sourceRID string, body *models.EnableVirtualTableSourceRequest) (*models.VirtualTableSourceLink, error) {
	provider := strings.TrimSpace(body.Provider)
	if !validVirtualProviders[provider] {
		return nil, fmt.Errorf("invalid provider: %s", body.Provider)
	}
	cfg := body.IcebergCatalogConfig
	if len(cfg) == 0 {
		cfg = []byte(`null`)
	}
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO virtual_table_sources_link (source_rid, provider, virtual_tables_enabled, iceberg_catalog_kind, iceberg_catalog_config)
		 VALUES ($1, $2, TRUE, $3, $4)
		 ON CONFLICT (source_rid) DO UPDATE SET virtual_tables_enabled = TRUE, provider = EXCLUDED.provider,
		     iceberg_catalog_kind = EXCLUDED.iceberg_catalog_kind, iceberg_catalog_config = EXCLUDED.iceberg_catalog_config,
		     updated_at = NOW()
		 RETURNING `+virtualTableSourceLinkColumns,
		sourceRID, provider, body.IcebergCatalogKind, cfg,
	)
	return scanVirtualTableSourceLink(row)
}

func (r *Repo) GetVirtualTableSourceLink(ctx context.Context, sourceRID string) (*models.VirtualTableSourceLink, error) {
	row := r.Pool.QueryRow(ctx, `SELECT `+virtualTableSourceLinkColumns+` FROM virtual_table_sources_link WHERE source_rid = $1`, sourceRID)
	link, err := scanVirtualTableSourceLink(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return link, err
}

func (r *Repo) CreateVirtualTable(ctx context.Context, sourceRID string, actorID string, body *models.CreateVirtualTableRequest) (*models.VirtualTable, error) {
	tableType := strings.TrimSpace(body.TableType)
	if !validVirtualTableTypes[tableType] {
		return nil, fmt.Errorf("invalid table_type: %s", body.TableType)
	}
	name := ""
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
	}
	if name == "" {
		name = body.Locator.DefaultDisplayName()
	}
	projectRID := strings.TrimSpace(body.ProjectRID)
	if name == "" || projectRID == "" {
		return nil, fmt.Errorf("project_rid and name/locator are required")
	}
	locator, err := body.Locator.CanonicalJSON()
	if err != nil {
		return nil, err
	}
	schema, err := normalizedRawJSON(body.SchemaInferred, "[]", "schema_inferred")
	if err != nil {
		return nil, err
	}
	capabilities, err := normalizedRawJSON(body.Capabilities, "{}", "capabilities")
	if err != nil {
		return nil, err
	}
	properties, err := normalizedRawJSON(body.Properties, "{}", "properties")
	if err != nil {
		return nil, err
	}
	actor := actorID
	owner := strings.TrimSpace(actorID)
	if body.Owner != nil && strings.TrimSpace(*body.Owner) != "" {
		owner = strings.TrimSpace(*body.Owner)
	}
	permissions, err := normalizedRawJSON(body.Permissions, string(defaultVirtualTablePermissions(owner)), "permissions")
	if err != nil {
		return nil, err
	}
	parentFolderRID := trimmedStringPtr(body.ParentFolderRID)
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO virtual_tables (id, source_rid, project_rid, name, parent_folder_rid, locator, table_type,
		     schema_inferred, capabilities, markings, properties, created_by)
		 SELECT $1, l.source_rid, $3, $4, $5, $6::jsonb, $7, $8::jsonb, $9::jsonb, $10,
		        COALESCE($11::jsonb, '{}'::jsonb) || jsonb_build_object(
		          'provider', l.provider,
		          'display_name', $4,
		          'external_reference', $6::jsonb,
		          'save_location', jsonb_build_object('project_rid', $3, 'parent_folder_rid', $5),
		          'source', jsonb_build_object('source_rid', l.source_rid, 'provider', l.provider),
		          'schema', $8::jsonb,
		          'owner', $12,
		          'permissions', $13::jsonb,
		          'registration', jsonb_build_object('mode', 'manual', 'registered_by', $14)
		        ),
		        $14
		 FROM virtual_table_sources_link l WHERE l.source_rid = $2 AND l.virtual_tables_enabled
		 RETURNING id, rid, source_rid, project_rid, name, parent_folder_rid, locator, table_type,
		           schema_inferred, capabilities, update_detection_enabled, update_detection_interval_seconds,
		           last_observed_version, last_polled_at,
		           COALESCE(update_detection_consecutive_failures, 0), update_detection_next_poll_at,
		           markings, properties, created_by, created_at, updated_at`,
		uuid.New(), sourceRID, projectRID, name, parentFolderRID,
		locator, tableType, schema, capabilities, body.Markings, properties, owner, permissions, actor,
	)
	v, err := scanVirtualTable(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if isUniqueViolation(err) {
		return nil, ErrConflict
	}
	return v, err
}

func (r *Repo) BulkRegisterVirtualTables(ctx context.Context, sourceRID string, actorID string, body *models.VirtualTableBulkRegisterRequest) (*models.VirtualTableBulkRegisterResponse, error) {
	link, err := r.GetVirtualTableSourceLink(ctx, sourceRID)
	if err != nil {
		return nil, err
	}
	if link == nil || !link.VirtualTablesEnabled {
		return nil, nil
	}
	if !bulkVirtualTableProviders[link.Provider] {
		return nil, fmt.Errorf("bulk registration is only supported for tabular providers")
	}
	if len(body.Entries) == 0 {
		return nil, fmt.Errorf("entries required")
	}
	projectRID := strings.TrimSpace(body.ProjectRID)
	response := &models.VirtualTableBulkRegisterResponse{
		Registered: []models.VirtualTable{},
		Errors:     []models.VirtualTableBulkError{},
	}
	for i := range body.Entries {
		entry := body.Entries[i]
		if strings.TrimSpace(entry.ProjectRID) == "" {
			entry.ProjectRID = projectRID
		}
		name := virtualTableErrorName(&entry)
		if entry.Locator.Kind != "tabular" {
			response.Errors = append(response.Errors, models.VirtualTableBulkError{Name: name, Error: "bulk registration requires tabular locators"})
			continue
		}
		v, err := r.CreateVirtualTable(ctx, sourceRID, actorID, &entry)
		if errors.Is(err, ErrConflict) {
			response.Errors = append(response.Errors, models.VirtualTableBulkError{Name: name, Error: "virtual table already registered"})
			continue
		}
		if err != nil {
			response.Errors = append(response.Errors, models.VirtualTableBulkError{Name: name, Error: err.Error()})
			continue
		}
		if v == nil {
			response.Errors = append(response.Errors, models.VirtualTableBulkError{Name: name, Error: "source not enabled"})
			continue
		}
		response.Registered = append(response.Registered, *v)
	}
	return response, nil
}

func (r *Repo) EnableVirtualTableAutoRegistration(ctx context.Context, sourceRID string, body *models.EnableAutoRegistrationRequest) (*models.VirtualTableSourceLink, error) {
	link, err := r.GetVirtualTableSourceLink(ctx, sourceRID)
	if err != nil {
		return nil, err
	}
	if link == nil || !link.VirtualTablesEnabled {
		return nil, nil
	}
	if !bulkVirtualTableProviders[link.Provider] {
		return nil, fmt.Errorf("auto-registration is only supported for tabular providers")
	}
	projectName := strings.TrimSpace(body.ProjectName)
	if projectName == "" {
		return nil, fmt.Errorf("project_name required")
	}
	layout := strings.TrimSpace(body.FolderMirrorKind)
	if layout == "" {
		layout = string(models.FolderMirrorKindNested)
	}
	if layout != string(models.FolderMirrorKindFlat) && layout != string(models.FolderMirrorKindNested) {
		return nil, fmt.Errorf("folder_mirror_kind must be FLAT or NESTED")
	}
	interval := int32(body.PollIntervalSeconds)
	if interval < 60 {
		return nil, fmt.Errorf("poll_interval_seconds must be at least 60")
	}
	projectRID := managedVirtualTableProjectRID(projectName)
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE virtual_table_sources_link
		    SET auto_register_enabled = TRUE,
		        auto_register_project_rid = $2,
		        auto_register_interval_seconds = $3,
		        auto_register_folder_mirror_kind = $4,
		        auto_register_table_tag_filters = $5,
		        auto_register_tag_filters = to_jsonb($5::text[]),
		        updated_at = NOW()
		  WHERE source_rid = $1 AND virtual_tables_enabled
		  RETURNING `+virtualTableSourceLinkColumns,
		sourceRID, projectRID, interval, layout, body.TableTagFilters,
	)
	return scanVirtualTableSourceLink(row)
}

func (r *Repo) DisableVirtualTableAutoRegistration(ctx context.Context, sourceRID string) error {
	_, err := r.Pool.Exec(
		ctx,
		`UPDATE virtual_table_sources_link
		    SET auto_register_enabled = FALSE,
		        updated_at = NOW()
		  WHERE source_rid = $1`,
		sourceRID,
	)
	return err
}

func (r *Repo) ScanVirtualTableAutoRegistrationNow(ctx context.Context, sourceRID string) (*models.AutoRegistrationScanSummary, error) {
	link, err := r.GetVirtualTableSourceLink(ctx, sourceRID)
	if err != nil {
		return nil, err
	}
	if link == nil || !link.AutoRegisterEnabled {
		return nil, nil
	}
	summary := &models.AutoRegistrationScanSummary{}
	err = r.Pool.QueryRow(
		ctx,
		`WITH run AS (
		    INSERT INTO auto_register_runs (source_rid, finished_at, status, added, updated, orphaned, errors)
		    VALUES ($1, NOW(), 'succeeded', 0, 0, 0, '[]'::jsonb)
		    RETURNING added, updated, orphaned
		  ), updated_link AS (
		    UPDATE virtual_table_sources_link
		       SET auto_register_last_run_at = NOW(),
		           auto_register_last_run_added = 0,
		           auto_register_last_run_updated = 0,
		           auto_register_last_run_orphaned = 0,
		           updated_at = NOW()
		     WHERE source_rid = $1
		  )
		  SELECT added, updated, orphaned FROM run`,
		sourceRID,
	).Scan(&summary.Added, &summary.Updated, &summary.Orphaned)
	if err != nil {
		return nil, err
	}
	return summary, nil
}

func (r *Repo) DiscoverVirtualTableCatalog(ctx context.Context, sourceRID string, path string) ([]models.DiscoveredEntry, error) {
	link, err := r.GetVirtualTableSourceLink(ctx, sourceRID)
	if err != nil {
		return nil, err
	}
	if link == nil || !link.VirtualTablesEnabled {
		return nil, nil
	}
	return virtualTableCatalogEntries(link.Provider, path), nil
}

func (r *Repo) ListVirtualTables(ctx context.Context, ownerID string, project, source, name, tableType string, limit int) ([]models.VirtualTable, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	query := virtualTableSelect + ` WHERE created_by = $1`
	args := []any{ownerID}
	if project != "" {
		args = append(args, project)
		query += fmt.Sprintf(` AND project_rid = $%d`, len(args))
	}
	if source != "" {
		args = append(args, source)
		query += fmt.Sprintf(` AND source_rid = $%d`, len(args))
	}
	if name != "" {
		args = append(args, "%"+strings.ToLower(name)+"%")
		query += fmt.Sprintf(` AND LOWER(name) LIKE $%d`, len(args))
	}
	if tableType != "" {
		args = append(args, tableType)
		query += fmt.Sprintf(` AND table_type = $%d`, len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, len(args))
	rows, err := r.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.VirtualTable, 0)
	for rows.Next() {
		v, err := scanVirtualTable(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

const virtualTableSelect = `SELECT id, rid, source_rid, project_rid, name, parent_folder_rid, locator, table_type,
	schema_inferred, capabilities, update_detection_enabled, update_detection_interval_seconds,
	last_observed_version, last_polled_at, COALESCE(update_detection_consecutive_failures, 0), update_detection_next_poll_at,
	markings, properties, created_by, created_at, updated_at FROM virtual_tables`

func (r *Repo) GetVirtualTable(ctx context.Context, rid string, ownerID string) (*models.VirtualTable, error) {
	row := r.Pool.QueryRow(ctx, virtualTableSelect+` WHERE rid = $1 AND created_by = $2`, rid, ownerID)
	v, err := scanVirtualTable(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) SetVirtualTableUpdateDetection(ctx context.Context, rid string, ownerID string, body *models.UpdateDetectionToggle) (*models.VirtualTable, error) {
	interval := int32(body.IntervalSeconds)
	if body.Enabled && interval < 60 {
		interval = 60
	}
	var intervalArg any
	var nextPollArg any
	if body.Enabled {
		intervalArg = interval
		nextPollArg = time.Now().UTC()
	}
	row := r.Pool.QueryRow(
		ctx,
		`WITH updated AS (
		   UPDATE virtual_tables
		      SET update_detection_enabled = $3,
		          update_detection_interval_seconds = $4,
		          update_detection_next_poll_at = $5,
		          updated_at = NOW()
		    WHERE rid = $1 AND created_by = $2
		    RETURNING rid
		 ) `+virtualTableSelect+` WHERE rid = (SELECT rid FROM updated)`,
		rid, ownerID, body.Enabled, intervalArg, nextPollArg,
	)
	v, err := scanVirtualTable(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) PollVirtualTableUpdateDetection(ctx context.Context, rid string, ownerID string) (*models.PollResult, error) {
	v, err := r.GetVirtualTable(ctx, rid, ownerID)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	start := time.Now()
	previous := v.LastObservedVersion
	observed := virtualTableObservedVersion(v)
	outcome := models.PollOutcomePotentialUpdate
	changeDetected := true
	if observed != nil {
		switch {
		case previous == nil:
			outcome = models.PollOutcomeInitial
		case *previous == *observed:
			outcome = models.PollOutcomeUnchanged
			changeDetected = false
		default:
			outcome = models.PollOutcomeChanged
		}
	}
	latencyMS := int32(time.Since(start).Milliseconds())
	if latencyMS == 0 {
		latencyMS = 1
	}
	nextPollAt := time.Now().UTC().Add(time.Duration(virtualTableUpdateDetectionInterval(v)) * time.Second)
	_, err = r.Pool.Exec(
		ctx,
		`UPDATE virtual_tables
		    SET last_observed_version = $2,
		        last_polled_at = NOW(),
		        update_detection_next_poll_at = $3,
		        update_detection_consecutive_failures = 0,
		        updated_at = NOW()
		  WHERE id = $1`,
		v.ID, observed, nextPollAt,
	)
	if err != nil {
		return nil, err
	}
	_, err = r.Pool.Exec(
		ctx,
		`INSERT INTO update_detection_polls (virtual_table_id, observed_version, change_detected, latency_ms)
		 VALUES ($1, $2, $3, $4)`,
		v.ID, observed, changeDetected, latencyMS,
	)
	if err != nil {
		return nil, err
	}
	lineage, err := r.GetVirtualTableLineage(ctx, rid, ownerID)
	if err != nil {
		return nil, err
	}
	builds := []models.VirtualTableDownstreamBuildPlan{}
	if lineage != nil {
		builds = downstreamBuildPlans(lineage.Nodes, outcome)
	}
	return &models.PollResult{
		VirtualTableRID:  v.RID,
		Outcome:          outcome,
		ObservedVersion:  observed,
		PreviousVersion:  previous,
		LatencyMS:        latencyMS,
		ChangeDetected:   changeDetected,
		EventEmitted:     outcome != models.PollOutcomeUnchanged,
		DownstreamBuilds: builds,
	}, nil
}

func (r *Repo) ListVirtualTableUpdateDetectionHistory(ctx context.Context, rid string, ownerID string, limit int) ([]models.PollHistoryRow, error) {
	v, err := r.GetVirtualTable(ctx, rid, ownerID)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := r.Pool.Query(
		ctx,
		`SELECT id, virtual_table_id, polled_at, observed_version, change_detected, latency_ms, error_message
		   FROM update_detection_polls
		  WHERE virtual_table_id = $1
		  ORDER BY polled_at DESC
		  LIMIT $2`,
		v.ID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PollHistoryRow{}
	for rows.Next() {
		var row models.PollHistoryRow
		if err := rows.Scan(&row.ID, &row.VirtualTableID, &row.PolledAt, &row.ObservedVersion, &row.ChangeDetected, &row.LatencyMS, &row.ErrorMessage); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repo) GetVirtualTableLineage(ctx context.Context, rid string, ownerID string) (*models.VirtualTableLineageResponse, error) {
	v, err := r.GetVirtualTable(ctx, rid, ownerID)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	response := defaultVirtualTableLineage(v)
	rows, err := r.Pool.Query(ctx, `SELECT project_rid, imported_by, imported_at FROM virtual_table_imports WHERE virtual_table_id = $1 ORDER BY imported_at DESC`, v.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var projectRID string
		var importedBy *string
		var importedAt time.Time
		if scanErr := rows.Scan(&projectRID, &importedBy, &importedAt); scanErr != nil {
			return nil, scanErr
		}
		meta, _ := json.Marshal(map[string]any{"imported_by": importedBy, "imported_at": importedAt})
		response.Nodes = append(response.Nodes, models.VirtualTableLineageNode{RID: projectRID, Kind: "project_import", DisplayName: "Imported in " + projectRID, Status: "active", Metadata: meta})
		response.Edges = append(response.Edges, models.VirtualTableLineageEdge{FromRID: v.RID, ToRID: projectRID, Kind: "imported_into"})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	outcome := models.PollOutcomeInitial
	var latestChanged bool
	var latestError *string
	err = r.Pool.QueryRow(
		ctx,
		`SELECT change_detected, error_message
		   FROM update_detection_polls
		  WHERE virtual_table_id = $1
		  ORDER BY polled_at DESC
		  LIMIT 1`,
		v.ID,
	).Scan(&latestChanged, &latestError)
	if err == nil {
		switch {
		case latestError != nil:
			outcome = models.PollOutcomeFailed
		case !latestChanged:
			outcome = models.PollOutcomeUnchanged
		default:
			outcome = models.PollOutcomeChanged
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	response.DownstreamBuilds = downstreamBuildPlans(response.Nodes, outcome)
	return response, nil
}

func virtualTableObservedVersion(v *models.VirtualTable) *string {
	var caps models.Capabilities
	_ = json.Unmarshal(v.Capabilities, &caps)
	if !caps.Versioning {
		return nil
	}
	var props map[string]json.RawMessage
	if json.Unmarshal(v.Properties, &props) == nil {
		for _, key := range []string{"source_version", "last_source_signature", "observed_version"} {
			if raw, ok := props[key]; ok {
				var value string
				if json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
					trimmed := strings.TrimSpace(value)
					return &trimmed
				}
			}
		}
		if raw, ok := props["update_detection"]; ok {
			var nested map[string]string
			if json.Unmarshal(raw, &nested) == nil {
				for _, key := range []string{"source_version", "last_source_signature", "observed_version"} {
					if value := strings.TrimSpace(nested[key]); value != "" {
						return &value
					}
				}
			}
		}
	}
	digest := sha256.Sum256([]byte(string(v.Locator) + "\n" + string(v.SchemaInferred)))
	value := fmt.Sprintf("sha256:%x", digest[:])
	return &value
}

func virtualTableUpdateDetectionInterval(v *models.VirtualTable) int32 {
	if v.UpdateDetectionIntervalSeconds != nil && *v.UpdateDetectionIntervalSeconds >= 60 {
		return *v.UpdateDetectionIntervalSeconds
	}
	return 3600
}

func defaultVirtualTableLineage(v *models.VirtualTable) *models.VirtualTableLineageResponse {
	short := strings.ReplaceAll(v.ID.String(), "-", "")
	if len(short) > 12 {
		short = short[:12]
	}
	sourceMeta, _ := json.Marshal(map[string]any{"source_rid": v.SourceRID})
	vtMeta, _ := json.Marshal(map[string]any{"project_rid": v.ProjectRID, "locator": json.RawMessage(v.Locator)})
	pipelineRID := "ri.foundry.main.pipeline.virtual-table-" + short
	datasetRID := "ri.foundry.main.dataset.virtual-table-" + short
	objectRID := "ri.ontology.main.object-type." + slugRIDComponent(v.Name)
	nodes := []models.VirtualTableLineageNode{
		{RID: v.SourceRID, Kind: "source", DisplayName: "Source " + v.SourceRID, Status: "active", Metadata: sourceMeta},
		{RID: v.RID, Kind: "virtual_table", DisplayName: v.Name, Status: "active", Metadata: vtMeta},
		{RID: pipelineRID, Kind: "pipeline", DisplayName: v.Name + " pipeline", Status: "listening"},
		{RID: datasetRID, Kind: "dataset", DisplayName: v.Name + " dataset output", Status: "materialized"},
		{RID: objectRID, Kind: "object_type", DisplayName: v.Name + " object output", Status: "indexed"},
	}
	edges := []models.VirtualTableLineageEdge{
		{FromRID: v.SourceRID, ToRID: v.RID, Kind: "backs"},
		{FromRID: v.RID, ToRID: pipelineRID, Kind: "pipeline_input"},
		{FromRID: pipelineRID, ToRID: datasetRID, Kind: "writes_dataset"},
		{FromRID: datasetRID, ToRID: objectRID, Kind: "indexes_object"},
	}
	return &models.VirtualTableLineageResponse{
		VirtualTableRID:        v.RID,
		SourceRID:              v.SourceRID,
		UpdateDetectionEnabled: v.UpdateDetectionEnabled,
		LastObservedVersion:    v.LastObservedVersion,
		Nodes:                  nodes,
		Edges:                  edges,
		DownstreamBuilds:       []models.VirtualTableDownstreamBuildPlan{},
	}
}

func downstreamBuildPlans(nodes []models.VirtualTableLineageNode, outcome models.PollOutcome) []models.VirtualTableDownstreamBuildPlan {
	action := "triggered"
	reason := "source-side update detected"
	switch outcome {
	case models.PollOutcomeUnchanged:
		action = "skipped"
		reason = "observed source version is unchanged"
	case models.PollOutcomePotentialUpdate:
		action = "triggered"
		reason = "source does not expose a comparable version; conservative update event emitted"
	case models.PollOutcomeInitial:
		action = "triggered"
		reason = "first observed source version"
	case models.PollOutcomeFailed:
		action = "skipped"
		reason = "poll failed before downstream event emission"
	}
	out := []models.VirtualTableDownstreamBuildPlan{}
	for _, node := range nodes {
		switch node.Kind {
		case "pipeline", "dataset", "object_type":
			out = append(out, models.VirtualTableDownstreamBuildPlan{
				TargetRID:   node.RID,
				TargetKind:  node.Kind,
				DisplayName: node.DisplayName,
				Action:      action,
				Reason:      reason,
			})
		}
	}
	return out
}

func slugRIDComponent(value string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "virtual-table"
	}
	return out
}

func normalizedRawJSON(raw json.RawMessage, fallback string, field string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage(fallback), nil
	}
	if !json.Valid([]byte(trimmed)) {
		return nil, fmt.Errorf("%s must be valid JSON", field)
	}
	return json.RawMessage(trimmed), nil
}

func defaultVirtualTablePermissions(owner string) json.RawMessage {
	payload := map[string][]string{
		"admins":  {},
		"owners":  {},
		"readers": {},
		"writers": {},
	}
	if owner != "" {
		payload["owners"] = []string{owner}
	}
	out, _ := json.Marshal(payload)
	return out
}

func trimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func virtualTableErrorName(body *models.CreateVirtualTableRequest) string {
	if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
		return strings.TrimSpace(*body.Name)
	}
	if name := body.Locator.DefaultDisplayName(); name != "" {
		return name
	}
	return "entry"
}

func managedVirtualTableProjectRID(projectName string) string {
	slug := strings.Builder{}
	lastDash := false
	for _, r := range strings.ToLower(projectName) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			slug.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && slug.Len() > 0 {
			slug.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(slug.String(), "-")
	if out == "" {
		out = "virtual-tables"
	}
	return "ri.foundry.main.project." + out
}

func virtualTableCatalogEntries(provider string, path string) []models.DiscoveredEntry {
	clean := strings.Trim(strings.TrimSpace(path), "/")
	parts := []string{}
	if clean != "" {
		parts = strings.Split(clean, "/")
	}
	switch provider {
	case "BIGQUERY", "SNOWFLAKE", "DATABRICKS":
		return tabularCatalogEntries(provider, parts)
	case "FOUNDRY_ICEBERG":
		return icebergCatalogEntries(parts)
	case "AMAZON_S3", "AZURE_ABFS", "GCS":
		return objectStoreCatalogEntries(parts)
	default:
		return []models.DiscoveredEntry{}
	}
}

func tabularCatalogEntries(provider string, parts []string) []models.DiscoveredEntry {
	if len(parts) == 0 {
		return []models.DiscoveredEntry{
			discoveredEntry("analytics", "analytics", "database", false, ""),
			discoveredEntry("operations", "operations", "database", false, ""),
		}
	}
	if len(parts) == 1 {
		return []models.DiscoveredEntry{
			discoveredEntry("public", parts[0]+"/public", "schema", false, ""),
			discoveredEntry("staging", parts[0]+"/staging", "schema", false, ""),
		}
	}
	if len(parts) == 2 {
		tableType := "TABLE"
		viewType := "VIEW"
		if provider == "DATABRICKS" {
			tableType = "MANAGED_DELTA"
		}
		return []models.DiscoveredEntry{
			discoveredEntry("orders", strings.Join(append(parts, "orders"), "/"), "table", true, tableType),
			discoveredEntry("customers", strings.Join(append(parts, "customers"), "/"), "table", true, tableType),
			discoveredEntry("daily_revenue", strings.Join(append(parts, "daily_revenue"), "/"), "view", true, viewType),
		}
	}
	return []models.DiscoveredEntry{}
}

func icebergCatalogEntries(parts []string) []models.DiscoveredEntry {
	if len(parts) == 0 {
		return []models.DiscoveredEntry{discoveredEntry("main", "main", "iceberg_namespace", false, "")}
	}
	if len(parts) == 1 {
		return []models.DiscoveredEntry{
			discoveredEntry("orders_iceberg", parts[0]+"/orders_iceberg", "iceberg_table", true, "MANAGED_ICEBERG"),
			discoveredEntry("events_iceberg", parts[0]+"/events_iceberg", "iceberg_table", true, "MANAGED_ICEBERG"),
		}
	}
	return []models.DiscoveredEntry{}
}

func objectStoreCatalogEntries(parts []string) []models.DiscoveredEntry {
	if len(parts) == 0 {
		return []models.DiscoveredEntry{
			discoveredEntry("warehouse", "warehouse", "file_prefix", false, ""),
			discoveredEntry("landing", "landing", "file_prefix", false, ""),
		}
	}
	if len(parts) == 1 {
		return []models.DiscoveredEntry{
			discoveredEntry("orders", parts[0]+"/orders", "file_prefix", true, "PARQUET_FILES"),
			discoveredEntry("events", parts[0]+"/events", "file_prefix", true, "PARQUET_FILES"),
		}
	}
	return []models.DiscoveredEntry{}
}

func discoveredEntry(displayName string, path string, kind string, registrable bool, tableType string) models.DiscoveredEntry {
	entry := models.DiscoveredEntry{
		DisplayName: displayName,
		Path:        path,
		Kind:        kind,
		Registrable: registrable,
	}
	if tableType != "" {
		entry.InferredTableType = &tableType
	}
	return entry
}

func scanVirtualTableSourceLink(r rowLikeT) (*models.VirtualTableSourceLink, error) {
	v := &models.VirtualTableSourceLink{}
	if err := r.Scan(&v.SourceRID, &v.Provider, &v.VirtualTablesEnabled, &v.CodeImportsEnabled,
		&v.ExportControls, &v.AutoRegisterProjectRID, &v.AutoRegisterEnabled, &v.AutoRegisterIntervalSeconds,
		&v.AutoRegisterTagFilters, &v.AutoRegisterFolderMirrorKind, &v.AutoRegisterTableTagFilters,
		&v.IcebergCatalogKind, &v.IcebergCatalogConfig, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	if v.AutoRegisterTableTagFilters == nil {
		v.AutoRegisterTableTagFilters = []string{}
	}
	return v, nil
}

func scanVirtualTable(r rowLikeT) (*models.VirtualTable, error) {
	v := &models.VirtualTable{}
	if err := r.Scan(&v.ID, &v.RID, &v.SourceRID, &v.ProjectRID, &v.Name, &v.ParentFolderRID,
		&v.Locator, &v.TableType, &v.SchemaInferred, &v.Capabilities, &v.UpdateDetectionEnabled,
		&v.UpdateDetectionIntervalSeconds, &v.LastObservedVersion, &v.LastPolledAt,
		&v.UpdateDetectionConsecutiveFailures, &v.UpdateDetectionNextPollAt, &v.Markings,
		&v.Properties, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	if v.Markings == nil {
		v.Markings = []string{}
	}
	return v, nil
}

const mediaSetSyncSelect = `SELECT m.id, m.source_id, m.sync_type, m.target_media_set_rid,
	m.subfolder, m.filters, m.schedule_cron, m.created_at
	FROM media_set_syncs m JOIN connections c ON c.id = m.source_id`

func (r *Repo) ListMediaSetSyncs(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.MediaSetSync, error) {
	rows, err := r.Pool.Query(ctx, mediaSetSyncSelect+` WHERE m.source_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+` ORDER BY m.created_at DESC`,
		sourceID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.MediaSetSync, 0)
	for rows.Next() {
		v, err := scanMediaSetSync(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetMediaSetSync(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.MediaSetSync, error) {
	row := r.Pool.QueryRow(ctx, mediaSetSyncSelect+` WHERE m.id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4"),
		id, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	v, err := scanMediaSetSync(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateMediaSetSync(ctx context.Context, sourceID uuid.UUID, body *models.CreateMediaSetSyncRequest, ownerID uuid.UUID) (*models.MediaSetSync, error) {
	filters, err := json.Marshal(body.Filters)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO media_set_syncs (id, source_id, sync_type, target_media_set_rid, subfolder, filters, schedule_cron)
			 SELECT $1, c.id, $3, $4, $5, $6, $7 FROM connections c WHERE c.id = $2 AND `+sourceAccessSQL("c", "$8", "$9", "$10")+`
			 RETURNING id, source_id, sync_type, target_media_set_rid, subfolder, filters, schedule_cron, created_at`,
		uuid.New(), sourceID, string(body.Kind), strings.TrimSpace(body.TargetMediaSetRID), strings.Trim(body.Subfolder, "/"), filters, body.ScheduleCron,
		ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleSyncCreate),
	)
	v, err := scanMediaSetSync(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) UpdateMediaSetSync(ctx context.Context, id uuid.UUID, body *models.UpdateMediaSetSyncRequest, ownerID uuid.UUID) (*models.MediaSetSync, error) {
	current, err := r.GetMediaSetSync(ctx, id, ownerID)
	if err != nil || current == nil {
		return current, err
	}
	kind := current.Kind
	if body.Kind != nil {
		kind = *body.Kind
	}
	target := current.TargetMediaSetRID
	if body.TargetMediaSetRID != nil {
		target = strings.TrimSpace(*body.TargetMediaSetRID)
	}
	subfolder := current.Subfolder
	if body.Subfolder != nil {
		subfolder = strings.Trim(*body.Subfolder, "/")
	}
	filters := current.Filters
	if body.Filters != nil {
		filters = *body.Filters
	}
	schedule := current.ScheduleCron
	if body.ScheduleCron != nil {
		schedule = body.ScheduleCron
	}
	if errs := models.ValidateMediaSetSyncConfig(kind, target, filters, schedule); len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	filtersJSON, err := json.Marshal(filters)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE media_set_syncs m SET sync_type = $2, target_media_set_rid = $3, subfolder = $4, filters = $5, schedule_cron = $6
			 FROM connections c WHERE m.source_id = c.id AND m.id = $1 AND `+sourceAccessSQL("c", "$7", "$8", "$9")+`
			 RETURNING m.id, m.source_id, m.sync_type, m.target_media_set_rid, m.subfolder, m.filters, m.schedule_cron, m.created_at`,
		id, string(kind), target, subfolder, filtersJSON, schedule,
		ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleEdit),
	)
	return scanMediaSetSync(row)
}

func scanMediaSetSync(r rowLikeT) (*models.MediaSetSync, error) {
	v := &models.MediaSetSync{}
	var kind string
	var filters []byte
	if err := r.Scan(&v.ID, &v.SourceID, &kind, &v.TargetMediaSetRID, &v.Subfolder, &filters, &v.ScheduleCron, &v.CreatedAt); err != nil {
		return nil, err
	}
	v.Kind = models.MediaSetSyncKind(kind)
	if len(filters) == 0 {
		filters = []byte(`{}`)
	}
	if err := json.Unmarshal(filters, &v.Filters); err != nil {
		return nil, err
	}
	return v, nil
}

const registrationSelect = `SELECT id, connection_id, selector, display_name, source_kind,
	registration_mode, auto_sync, update_detection, target_dataset_id, last_source_signature,
	last_dataset_version, metadata, created_at, updated_at FROM connection_registrations`

func (r *Repo) ListRegistrations(ctx context.Context, sourceID uuid.UUID) ([]models.ConnectionRegistration, error) {
	rows, err := r.Pool.Query(ctx, registrationSelect+` WHERE connection_id = $1 ORDER BY created_at DESC`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ConnectionRegistration{}
	for rows.Next() {
		v, err := scanRegistration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) UpsertRegistration(ctx context.Context, sourceID uuid.UUID, source models.DiscoveredSource, mode string, autoSync bool, updateDetection bool, targetDatasetID *uuid.UUID, metadata json.RawMessage) (*models.ConnectionRegistration, error) {
	if len(metadata) == 0 || string(metadata) == "null" {
		metadata = []byte(`{}`)
	}
	row := r.Pool.QueryRow(ctx, `WITH upserted AS (
		INSERT INTO connection_registrations (id, connection_id, selector, display_name, source_kind, registration_mode, auto_sync, update_detection, target_dataset_id, last_source_signature, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (connection_id, selector) DO UPDATE SET display_name = EXCLUDED.display_name, source_kind = EXCLUDED.source_kind,
			registration_mode = EXCLUDED.registration_mode, auto_sync = EXCLUDED.auto_sync, update_detection = EXCLUDED.update_detection,
			target_dataset_id = EXCLUDED.target_dataset_id, last_source_signature = EXCLUDED.last_source_signature,
			metadata = EXCLUDED.metadata, updated_at = NOW()
		RETURNING id, connection_id, selector, display_name, source_kind, registration_mode, auto_sync, update_detection, target_dataset_id, last_source_signature, last_dataset_version, metadata, created_at, updated_at)
		SELECT id, connection_id, selector, display_name, source_kind, registration_mode, auto_sync, update_detection, target_dataset_id, last_source_signature, last_dataset_version, metadata, created_at, updated_at FROM upserted`, uuid.New(), sourceID, source.Selector, source.DisplayName, source.SourceKind, mode, autoSync, updateDetection, targetDatasetID, source.SourceSignature, metadata)
	v, err := scanRegistration(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) GetRegistration(ctx context.Context, sourceID uuid.UUID, registrationID uuid.UUID) (*models.ConnectionRegistration, error) {
	row := r.Pool.QueryRow(ctx, registrationSelect+` WHERE connection_id = $1 AND id = $2`, sourceID, registrationID)
	v, err := scanRegistration(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) DeleteRegistration(ctx context.Context, sourceID uuid.UUID, registrationID uuid.UUID) (bool, error) {
	ct, err := r.Pool.Exec(ctx, `DELETE FROM connection_registrations WHERE connection_id = $1 AND id = $2`, sourceID, registrationID)
	return ct.RowsAffected() > 0, err
}

func (r *Repo) UpdateConnectionConfig(ctx context.Context, id uuid.UUID, config json.RawMessage) (*models.Connection, error) {
	row := r.Pool.QueryRow(ctx, `UPDATE connections SET config = $2, updated_at = NOW() WHERE id = $1 RETURNING id, name, connector_type, config, status, owner_id, last_sync_at, created_at, updated_at`, id, config)
	v, err := scanConnection(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) ListIcebergNamespaces(ctx context.Context) ([]models.Connection, error) {
	rows, err := r.Pool.Query(ctx, connectionSelect+` WHERE EXISTS (SELECT 1 FROM connection_registrations reg WHERE reg.connection_id = connections.id AND COALESCE((reg.metadata->>'supports_zero_copy')::bool, false) = true) ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Connection{}
	for rows.Next() {
		v, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetIcebergConnection(ctx context.Context, namespace string) (*models.Connection, error) {
	row := r.Pool.QueryRow(ctx, connectionSelect+` WHERE name = $1 OR regexp_replace(name, '[^A-Za-z0-9_-]', '_', 'g') = $1 LIMIT 1`, namespace)
	v, err := scanConnection(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) ListIcebergTables(ctx context.Context, connectionID uuid.UUID) ([]models.ConnectionRegistration, error) {
	rows, err := r.Pool.Query(ctx, registrationSelect+` WHERE connection_id = $1 AND COALESCE((metadata->>'supports_zero_copy')::bool, false) = true ORDER BY selector`, connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ConnectionRegistration{}
	for rows.Next() {
		v, err := scanRegistration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func scanRegistration(r rowLikeT) (*models.ConnectionRegistration, error) {
	v := &models.ConnectionRegistration{}
	if err := r.Scan(&v.ID, &v.ConnectionID, &v.Selector, &v.DisplayName, &v.SourceKind, &v.RegistrationMode, &v.AutoSync, &v.UpdateDetection, &v.TargetDatasetID, &v.LastSourceSignature, &v.LastDatasetVersion, &v.Metadata, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

func (r *Repo) GetRegistrationSignature(ctx context.Context, sourceID uuid.UUID, selector string) (*string, error) {
	row := r.Pool.QueryRow(ctx, `SELECT last_source_signature FROM connection_registrations WHERE connection_id = $1 AND selector = $2`, sourceID, selector)
	var sig *string
	if err := row.Scan(&sig); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return sig, nil
}

func (r *Repo) RecordRegistrationSignature(ctx context.Context, sourceID uuid.UUID, selector string, signature *string) error {
	if signature == nil {
		return nil
	}
	_, err := r.Pool.Exec(ctx, `UPDATE connection_registrations SET last_source_signature = $3, updated_at = NOW() WHERE connection_id = $1 AND selector = $2`, sourceID, selector, *signature)
	return err
}

func (r *Repo) RunDueSyncJobs(ctx context.Context, now time.Time) (int, error) {
	// Rust's scheduler delegates to sync_engine::run_due_jobs. The local sync
	// runtime is disabled in this Go slice, so this remains a compatibility
	// no-op until the ingestion-replication runtime is ported.
	_ = ctx
	_ = now
	return 0, nil
}

const connectorAgentSelect = `SELECT id, name, agent_url, version, environment, host, owner_id, status,
	capabilities, metadata, connected_sources, supported_connector_capabilities, assigned_proxy_policies,
	connection_failures, last_heartbeat_at, created_at, updated_at FROM connector_agents`

func (r *Repo) ListConnectorAgents(ctx context.Context, ownerID uuid.UUID) ([]models.ConnectorAgent, error) {
	rows, err := r.Pool.Query(ctx, connectorAgentSelect+` WHERE owner_id = $1 ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ConnectorAgent{}
	for rows.Next() {
		agent, err := scanConnectorAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *agent)
	}
	return out, rows.Err()
}

func (r *Repo) RegisterConnectorAgent(ctx context.Context, body *models.RegisterAgentRequest, ownerID uuid.UUID) (*models.ConnectorAgent, error) {
	id := uuid.New()
	connectedSources, supportedCapabilities, proxyPolicies, failures, err := marshalAgentPayloads(body.ConnectedSources, body.SupportedConnectorCapabilities, body.AssignedProxyPolicies, body.ConnectionFailures, false)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO connector_agents
			(id, name, agent_url, version, environment, host, owner_id, status, capabilities, metadata,
			 connected_sources, supported_connector_capabilities, assigned_proxy_policies, connection_failures, last_heartbeat_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'online', $8, $9, $10, $11, $12, $13, NOW())
		 ON CONFLICT (agent_url) DO UPDATE SET
			name = EXCLUDED.name,
			version = EXCLUDED.version,
			environment = EXCLUDED.environment,
			host = EXCLUDED.host,
			owner_id = EXCLUDED.owner_id,
			status = 'online',
			capabilities = EXCLUDED.capabilities,
			metadata = EXCLUDED.metadata,
			connected_sources = EXCLUDED.connected_sources,
			supported_connector_capabilities = EXCLUDED.supported_connector_capabilities,
			assigned_proxy_policies = EXCLUDED.assigned_proxy_policies,
			connection_failures = EXCLUDED.connection_failures,
			last_heartbeat_at = NOW(),
			updated_at = NOW()
		 WHERE connector_agents.owner_id = EXCLUDED.owner_id
		 RETURNING id, name, agent_url, version, environment, host, owner_id, status,
		           capabilities, metadata, connected_sources, supported_connector_capabilities, assigned_proxy_policies,
		           connection_failures, last_heartbeat_at, created_at, updated_at`,
		id, strings.TrimSpace(body.Name), strings.TrimSpace(body.AgentURL), body.Version, body.Environment, body.Host,
		ownerID, body.Capabilities, body.Metadata, connectedSources, supportedCapabilities, proxyPolicies, failures,
	)
	agent, err := scanConnectorAgent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return agent, err
}

func (r *Repo) HeartbeatConnectorAgent(ctx context.Context, id uuid.UUID, body *models.AgentHeartbeatRequest, ownerID uuid.UUID) (*models.ConnectorAgent, error) {
	connectedSources, supportedCapabilities, proxyPolicies, failures, err := marshalAgentPayloads(body.ConnectedSources, body.SupportedConnectorCapabilities, body.AssignedProxyPolicies, body.ConnectionFailures, true)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(
		ctx,
		`UPDATE connector_agents
		 SET status = 'online',
		     capabilities = $3,
		     metadata = $4,
		     version = COALESCE(NULLIF($5, ''), version),
		     environment = COALESCE(NULLIF($6, ''), environment),
		     host = COALESCE(NULLIF($7, ''), host),
		     connected_sources = CASE WHEN $8::jsonb = 'null'::jsonb THEN connected_sources ELSE $8::jsonb END,
		     supported_connector_capabilities = CASE WHEN $9::jsonb = 'null'::jsonb THEN supported_connector_capabilities ELSE $9::jsonb END,
		     assigned_proxy_policies = CASE WHEN $10::jsonb = 'null'::jsonb THEN assigned_proxy_policies ELSE $10::jsonb END,
		     connection_failures = CASE WHEN $11::jsonb = 'null'::jsonb THEN connection_failures ELSE $11::jsonb END,
		     last_heartbeat_at = NOW(),
		     updated_at = NOW()
		 WHERE id = $1 AND owner_id = $2
		 RETURNING id, name, agent_url, version, environment, host, owner_id, status,
		           capabilities, metadata, connected_sources, supported_connector_capabilities, assigned_proxy_policies,
		           connection_failures, last_heartbeat_at, created_at, updated_at`,
		id, ownerID, body.Capabilities, body.Metadata, body.Version, body.Environment, body.Host,
		connectedSources, supportedCapabilities, proxyPolicies, failures,
	)
	agent, err := scanConnectorAgent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return agent, err
}

func (r *Repo) DeleteConnectorAgent(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM connector_agents WHERE id = $1 AND owner_id = $2`, id, ownerID)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func (r *Repo) GetConnectorAgent(ctx context.Context, id uuid.UUID) (*models.ConnectorAgent, error) {
	row := r.Pool.QueryRow(ctx, connectorAgentSelect+` WHERE id = $1`, id)
	agent, err := scanConnectorAgent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return agent, err
}

func scanConnectorAgent(r rowLikeT) (*models.ConnectorAgent, error) {
	agent := &models.ConnectorAgent{}
	var connectedSources, supportedCapabilities, proxyPolicies, failures []byte
	if err := r.Scan(
		&agent.ID,
		&agent.Name,
		&agent.AgentURL,
		&agent.Version,
		&agent.Environment,
		&agent.Host,
		&agent.OwnerID,
		&agent.Status,
		&agent.Capabilities,
		&agent.Metadata,
		&connectedSources,
		&supportedCapabilities,
		&proxyPolicies,
		&failures,
		&agent.LastHeartbeatAt,
		&agent.CreatedAt,
		&agent.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(connectedSources) > 0 {
		_ = json.Unmarshal(connectedSources, &agent.ConnectedSources)
	}
	if len(supportedCapabilities) > 0 {
		_ = json.Unmarshal(supportedCapabilities, &agent.SupportedConnectorCapabilities)
	}
	if len(proxyPolicies) > 0 {
		_ = json.Unmarshal(proxyPolicies, &agent.AssignedProxyPolicies)
	}
	if len(failures) > 0 {
		_ = json.Unmarshal(failures, &agent.ConnectionFailures)
	}
	normalized := models.NormalizeConnectorAgent(*agent)
	return &normalized, nil
}

func marshalAgentPayloads(connectedSources []models.AgentConnectedSource, supportedCapabilities []models.AgentConnectorCapabilitySummary, proxyPolicies []models.AgentProxyPolicyAssignment, failures []models.AgentConnectionFailure, nilAsNull bool) ([]byte, []byte, []byte, []byte, error) {
	if nilAsNull && connectedSources == nil {
		connectedSources = nil
	} else {
		connectedSources = models.NormalizeAgentConnectedSources(connectedSources)
	}
	if nilAsNull && supportedCapabilities == nil {
		supportedCapabilities = nil
	} else {
		supportedCapabilities = models.NormalizeAgentConnectorCapabilities(supportedCapabilities)
	}
	if nilAsNull && proxyPolicies == nil {
		proxyPolicies = nil
	} else {
		proxyPolicies = models.NormalizeAgentProxyPolicyAssignments(proxyPolicies)
	}
	if nilAsNull && failures == nil {
		failures = nil
	} else {
		failures = models.NormalizeAgentConnectionFailures(failures)
	}
	connectedSourcesJSON, err := json.Marshal(connectedSources)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	supportedCapabilitiesJSON, err := json.Marshal(supportedCapabilities)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	proxyPoliciesJSON, err := json.Marshal(proxyPolicies)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	failuresJSON, err := json.Marshal(failures)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return connectedSourcesJSON, supportedCapabilitiesJSON, proxyPoliciesJSON, failuresJSON, nil
}

const webhookHistorySelect = `SELECT id, source_id, user_id, status, http_status,
	input_policy, inputs, output_parameters, error, call_count, started_at,
	finished_at, duration_ms, retention_expires_at, created_at
	FROM webhook_invocation_history`

func (r *Repo) AppendWebhookHistory(ctx context.Context, body *models.CreateWebhookHistoryEntry) (*models.WebhookHistoryEntry, error) {
	if body == nil {
		return nil, errors.New("webhook history entry is nil")
	}
	if body.StartedAt.IsZero() {
		body.StartedAt = time.Now().UTC()
	}
	if body.FinishedAt.IsZero() {
		body.FinishedAt = time.Now().UTC()
	}
	if body.RetentionExpiresAt.IsZero() {
		body.RetentionExpiresAt = body.FinishedAt.Add(30 * 24 * time.Hour)
	}
	durationMS := body.FinishedAt.Sub(body.StartedAt).Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}
	policy, err := json.Marshal(body.InputPolicy)
	if err != nil {
		return nil, err
	}
	var httpStatus any
	if body.HTTPStatus != nil {
		httpStatus = int(*body.HTTPStatus)
	}
	id := uuid.New()
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO webhook_invocation_history
			(id, source_id, user_id, status, http_status, input_policy, inputs,
			 output_parameters, error, call_count, started_at, finished_at,
			 duration_ms, retention_expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING id, source_id, user_id, status, http_status, input_policy,
		           inputs, output_parameters, error, call_count, started_at,
		           finished_at, duration_ms, retention_expires_at, created_at`,
		id, body.SourceID, body.UserID, body.Status, httpStatus, policy,
		nullableJSON(body.Inputs), nullableJSON(body.OutputParameters), body.Error,
		body.CallCount, body.StartedAt, body.FinishedAt, durationMS, body.RetentionExpiresAt,
	)
	return scanWebhookHistory(row)
}

func (r *Repo) ListWebhookHistory(ctx context.Context, sourceID uuid.UUID, limit int) ([]models.WebhookHistoryEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	_, _ = r.Pool.Exec(ctx, `DELETE FROM webhook_invocation_history WHERE retention_expires_at < NOW()`)
	rows, err := r.Pool.Query(ctx, webhookHistorySelect+`
		WHERE source_id = $1 AND retention_expires_at >= NOW()
		ORDER BY created_at DESC
		LIMIT $2`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.WebhookHistoryEntry{}
	for rows.Next() {
		row, err := scanWebhookHistory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

func scanWebhookHistory(r rowLikeT) (*models.WebhookHistoryEntry, error) {
	entry := &models.WebhookHistoryEntry{}
	var policy json.RawMessage
	var httpStatus pgtype.Int4
	if err := r.Scan(
		&entry.ID,
		&entry.SourceID,
		&entry.UserID,
		&entry.Status,
		&httpStatus,
		&policy,
		&entry.Inputs,
		&entry.OutputParameters,
		&entry.Error,
		&entry.CallCount,
		&entry.StartedAt,
		&entry.FinishedAt,
		&entry.DurationMS,
		&entry.RetentionExpiresAt,
		&entry.CreatedAt,
	); err != nil {
		return nil, err
	}
	if len(policy) > 0 {
		_ = json.Unmarshal(policy, &entry.InputPolicy)
	}
	if httpStatus.Valid {
		converted := uint16(httpStatus.Int32)
		entry.HTTPStatus = &converted
	}
	if len(entry.Inputs) == 0 {
		entry.Inputs = nil
	}
	if len(entry.OutputParameters) == 0 {
		entry.OutputParameters = nil
	}
	return entry, nil
}

const inboundListenerEventSelect = `SELECT id, source_id, listener_id, event_id, status,
	signature_verified, payload, headers, destination, created_at
	FROM inbound_listener_events`

func (r *Repo) AppendInboundListenerEvent(ctx context.Context, body *models.CreateInboundListenerEvent) (*models.InboundListenerEvent, error) {
	if body == nil {
		return nil, errors.New("inbound listener event is nil")
	}
	if strings.TrimSpace(body.ListenerID) == "" {
		return nil, errors.New("listener_id is required")
	}
	status := strings.TrimSpace(body.Status)
	if status == "" {
		status = "accepted"
	}
	destination, err := json.Marshal(body.Destination)
	if err != nil {
		return nil, err
	}
	id := uuid.New()
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO inbound_listener_events
			(id, source_id, listener_id, event_id, status, signature_verified,
			 payload, headers, destination)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, source_id, listener_id, event_id, status, signature_verified,
		           payload, headers, destination, created_at`,
		id, body.SourceID, body.ListenerID, nullableText(body.EventID), status,
		body.SignatureVerified, nullableJSON(body.Payload), nullableJSON(body.Headers), destination,
	)
	return scanInboundListenerEvent(row)
}

func (r *Repo) ListInboundListenerEvents(ctx context.Context, sourceID uuid.UUID, limit int) ([]models.InboundListenerEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.Pool.Query(ctx, inboundListenerEventSelect+`
		WHERE source_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.InboundListenerEvent{}
	for rows.Next() {
		row, err := scanInboundListenerEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

func nullableText(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func scanInboundListenerEvent(r rowLikeT) (*models.InboundListenerEvent, error) {
	event := &models.InboundListenerEvent{}
	var eventID pgtype.Text
	var destination json.RawMessage
	if err := r.Scan(
		&event.ID,
		&event.SourceID,
		&event.ListenerID,
		&eventID,
		&event.Status,
		&event.SignatureVerified,
		&event.Payload,
		&event.Headers,
		&destination,
		&event.CreatedAt,
	); err != nil {
		return nil, err
	}
	if eventID.Valid {
		event.EventID = eventID.String
	}
	if len(destination) > 0 {
		_ = json.Unmarshal(destination, &event.Destination)
	}
	if len(event.Payload) == 0 {
		event.Payload = nil
	}
	if len(event.Headers) == 0 {
		event.Headers = nil
	}
	return event, nil
}

// SDC.40 — retry/recovery policy storage and recent-failure lookups. The
// policy table holds a JSONB map of failure category → backoff settings; the
// failures table records the most recent attempt context per sync run so the
// recovery summary can decide retry vs. escalate without re-deriving state
// from raw run logs.

func (r *Repo) GetSourceRetryPolicy(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) (*models.SourceRetryPolicy, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT p.source_id, p.categories, p.updated_by, p.updated_at
		   FROM source_retry_policies p
		   JOIN connections c ON c.id = p.source_id
		  WHERE p.source_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4"),
		sourceID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	var (
		id         uuid.UUID
		categories []byte
		updatedBy  *string
		updatedAt  time.Time
	)
	if err := row.Scan(&id, &categories, &updatedBy, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	policy := &models.SourceRetryPolicy{
		SourceID:   id,
		SourceRID:  models.SourceRIDForConnection(id),
		Categories: map[models.RetryFailureCategory]models.RetryBackoffPolicy{},
		UpdatedBy:  updatedBy,
		UpdatedAt:  updatedAt,
	}
	if len(categories) > 0 && string(categories) != "null" {
		if err := json.Unmarshal(categories, &policy.Categories); err != nil {
			return nil, fmt.Errorf("decode retry policy categories: %w", err)
		}
	}
	return policy, nil
}

func (r *Repo) UpsertSourceRetryPolicy(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, actorID *string, policy models.SourceRetryPolicy) (*models.SourceRetryPolicy, error) {
	allowed, err := r.CheckSourceRole(ctx, sourceID, ownerID, models.SourceRoleEdit)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, nil
	}
	normalized := models.NormalizeSourceRetryPolicy(policy, sourceID, time.Now().UTC())
	categoriesJSON, err := json.Marshal(normalized.Categories)
	if err != nil {
		return nil, fmt.Errorf("encode retry policy categories: %w", err)
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO source_retry_policies (source_id, categories, updated_by, updated_at)
		      VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (source_id) DO UPDATE
		    SET categories = EXCLUDED.categories,
		        updated_by = EXCLUDED.updated_by,
		        updated_at = NOW()
		 RETURNING source_id, categories, updated_by, updated_at`,
		sourceID, categoriesJSON, actorID)
	var (
		id        uuid.UUID
		stored    []byte
		updatedBy *string
		updatedAt time.Time
	)
	if err := row.Scan(&id, &stored, &updatedBy, &updatedAt); err != nil {
		return nil, err
	}
	out := &models.SourceRetryPolicy{
		SourceID:   id,
		SourceRID:  models.SourceRIDForConnection(id),
		Categories: map[models.RetryFailureCategory]models.RetryBackoffPolicy{},
		UpdatedBy:  updatedBy,
		UpdatedAt:  updatedAt,
	}
	if len(stored) > 0 && string(stored) != "null" {
		if err := json.Unmarshal(stored, &out.Categories); err != nil {
			return nil, fmt.Errorf("decode retry policy categories: %w", err)
		}
	}
	return out, nil
}

func (r *Repo) RecordSyncRunFailure(ctx context.Context, runID uuid.UUID, ownerID uuid.UUID, failure models.RetryRecoveryRunSummary) (*models.RetryRecoveryRunSummary, error) {
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO sync_run_failures
		    (run_id, sync_def_id, attempt, max_attempts, failure_category, error_message,
		     next_retry_at, has_checkpoint, checkpoint_summary, escalated, recorded_at)
		 SELECT $1, r.sync_def_id, $2, $3, $4, $5, $6, $7, $8, $9, NOW()
		   FROM sync_runs r
		   JOIN batch_sync_defs d ON d.id = r.sync_def_id
		   JOIN connections c ON c.id = d.source_id
		  WHERE r.id = $1 AND `+sourceAccessSQL("c", "$10", "$11", "$12")+`
		 ON CONFLICT (run_id) DO UPDATE
		    SET attempt            = EXCLUDED.attempt,
		        max_attempts       = EXCLUDED.max_attempts,
		        failure_category   = EXCLUDED.failure_category,
		        error_message      = EXCLUDED.error_message,
		        next_retry_at      = EXCLUDED.next_retry_at,
		        has_checkpoint     = EXCLUDED.has_checkpoint,
		        checkpoint_summary = EXCLUDED.checkpoint_summary,
		        escalated          = EXCLUDED.escalated,
		        recorded_at        = NOW()
		 RETURNING run_id, sync_def_id, attempt, max_attempts, failure_category, error_message,
		           next_retry_at, has_checkpoint, checkpoint_summary, escalated, recorded_at`,
		runID, failure.Attempt, failure.MaxAttempts, string(failure.Category), failure.Error,
		failure.NextRetryAt, failure.HasCheckpoint, failure.CheckpointSummary, failure.Escalated,
		ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleUse))
	return scanSyncRunFailure(row)
}

func (r *Repo) ListSyncRunFailuresForSource(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, limit int) ([]models.RetryRecoveryRunSummary, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT f.run_id, f.sync_def_id, f.attempt, f.max_attempts, f.failure_category, f.error_message,
		        f.next_retry_at, f.has_checkpoint, f.checkpoint_summary, f.escalated, f.recorded_at,
		        r.status, r.started_at, r.finished_at
		   FROM sync_run_failures f
		   JOIN sync_runs r        ON r.id = f.run_id
		   JOIN batch_sync_defs d  ON d.id = f.sync_def_id
		   JOIN connections c      ON c.id = d.source_id
		  WHERE d.source_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+`
		  ORDER BY f.recorded_at DESC
		  LIMIT $5`,
		sourceID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.RetryRecoveryRunSummary, 0)
	for rows.Next() {
		v, err := scanRetrySummaryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func scanSyncRunFailure(r rowLikeT) (*models.RetryRecoveryRunSummary, error) {
	v := &models.RetryRecoveryRunSummary{}
	var (
		category   string
		errorMsg   *string
		checkpoint *string
		recordedAt time.Time
	)
	if err := r.Scan(&v.RunID, &v.SyncDefID, &v.Attempt, &v.MaxAttempts, &category, &errorMsg,
		&v.NextRetryAt, &v.HasCheckpoint, &checkpoint, &v.Escalated, &recordedAt); err != nil {
		return nil, err
	}
	v.Category = models.RetryFailureCategory(category)
	if errorMsg != nil {
		v.Error = *errorMsg
	}
	if checkpoint != nil {
		v.CheckpointSummary = *checkpoint
	}
	v.StartedAt = recordedAt
	return v, nil
}

func scanRetrySummaryRow(r rowLikeT) (*models.RetryRecoveryRunSummary, error) {
	v := &models.RetryRecoveryRunSummary{}
	var (
		category   string
		errorMsg   *string
		checkpoint *string
		recordedAt time.Time
		status     string
		startedAt  time.Time
		finishedAt *time.Time
	)
	if err := r.Scan(&v.RunID, &v.SyncDefID, &v.Attempt, &v.MaxAttempts, &category, &errorMsg,
		&v.NextRetryAt, &v.HasCheckpoint, &checkpoint, &v.Escalated, &recordedAt,
		&status, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	v.Category = models.RetryFailureCategory(category)
	if errorMsg != nil {
		v.Error = *errorMsg
	}
	if checkpoint != nil {
		v.CheckpointSummary = *checkpoint
	}
	v.Status = status
	v.StartedAt = startedAt
	v.FinishedAt = finishedAt
	_ = recordedAt
	return v, nil
}

// SDC.41 — Media sync handoff run history and usage rollup. Persists each
// execution and exposes an aggregate per-sync usage view.

const mediaSetSyncRunSelect = `SELECT r.id, r.sync_def_id, r.status, r.started_at, r.finished_at,
	r.accepted_files, r.skipped_files, r.schema_mismatched, r.dispatched_files, r.dispatch_errors,
	r.bytes_accepted, r.selected_paths, r.schema_mismatches, r.error_message, r.triggered_by
	FROM media_set_sync_runs r
	JOIN media_set_syncs m ON m.id = r.sync_def_id
	JOIN connections c ON c.id = m.source_id`

func (r *Repo) RecordMediaSetSyncRun(ctx context.Context, syncID uuid.UUID, ownerID uuid.UUID, run models.MediaSetSyncRun) (*models.MediaSetSyncRun, error) {
	allowed, err := r.CheckSourceRoleForMediaSetSync(ctx, syncID, ownerID, models.SourceRoleUse)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, nil
	}
	selectedPathsJSON, err := json.Marshal(run.SelectedPaths)
	if err != nil {
		return nil, fmt.Errorf("encode selected paths: %w", err)
	}
	mismatchesJSON, err := json.Marshal(run.SchemaMismatches)
	if err != nil {
		return nil, fmt.Errorf("encode schema mismatches: %w", err)
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO media_set_sync_runs
		    (id, sync_def_id, status, started_at, finished_at,
		     accepted_files, skipped_files, schema_mismatched, dispatched_files, dispatch_errors,
		     bytes_accepted, selected_paths, schema_mismatches, error_message, triggered_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb, $14, $15)
		 RETURNING id, sync_def_id, status, started_at, finished_at,
		           accepted_files, skipped_files, schema_mismatched, dispatched_files, dispatch_errors,
		           bytes_accepted, selected_paths, schema_mismatches, error_message, triggered_by`,
		uuid.New(), syncID, string(run.Status), run.StartedAt, run.FinishedAt,
		run.AcceptedFiles, run.SkippedFiles, run.SchemaMismatched, run.DispatchedFiles, run.DispatchErrors,
		run.BytesAccepted, selectedPathsJSON, mismatchesJSON, run.ErrorMessage, run.TriggeredBy)
	return scanMediaSetSyncRun(row)
}

func (r *Repo) ListMediaSetSyncRuns(ctx context.Context, syncID uuid.UUID, ownerID uuid.UUID, limit int) ([]models.MediaSetSyncRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.Pool.Query(ctx,
		mediaSetSyncRunSelect+`
		  WHERE r.sync_def_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+`
		  ORDER BY r.started_at DESC
		  LIMIT $5`,
		syncID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.MediaSetSyncRun{}
	for rows.Next() {
		v, err := scanMediaSetSyncRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) MediaSetSyncUsageForSource(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) (map[uuid.UUID]models.MediaSetSyncUsageSummary, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT r.sync_def_id,
		        COUNT(*)                                         AS run_count,
		        MAX(r.started_at)                                AS last_run_at,
		        COALESCE(SUM(r.accepted_files), 0)               AS total_accepted_files,
		        COALESCE(SUM(r.bytes_accepted), 0)               AS total_bytes_accepted,
		        COALESCE(SUM(r.dispatch_errors), 0)              AS total_dispatch_errors,
		        COALESCE(SUM(r.schema_mismatched), 0)            AS total_schema_mismatch
		   FROM media_set_sync_runs r
		   JOIN media_set_syncs m ON m.id = r.sync_def_id
		   JOIN connections c     ON c.id = m.source_id
		  WHERE m.source_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+`
		  GROUP BY r.sync_def_id`,
		sourceID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	usage := map[uuid.UUID]models.MediaSetSyncUsageSummary{}
	for rows.Next() {
		summary := models.MediaSetSyncUsageSummary{}
		var lastRunAt *time.Time
		var totalAccepted, totalBytes, totalDispatchErrors, totalSchemaMismatch int64
		var runCount int64
		if err := rows.Scan(&summary.SyncDefID, &runCount, &lastRunAt,
			&totalAccepted, &totalBytes, &totalDispatchErrors, &totalSchemaMismatch); err != nil {
			return nil, err
		}
		summary.RunCount = uint32(runCount)
		summary.LastRunAt = lastRunAt
		summary.TotalAcceptedFiles = uint64(totalAccepted)
		summary.TotalBytesAccepted = uint64(totalBytes)
		summary.TotalDispatchErrors = uint64(totalDispatchErrors)
		summary.TotalSchemaMismatch = uint64(totalSchemaMismatch)
		usage[summary.SyncDefID] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Backfill last status + error from the most recent run per def. The two
	// queries are cheap and let the usage view stay denormalized.
	statusRows, err := r.Pool.Query(ctx,
		`SELECT DISTINCT ON (r.sync_def_id) r.sync_def_id, r.status, r.error_message
		   FROM media_set_sync_runs r
		   JOIN media_set_syncs m ON m.id = r.sync_def_id
		   JOIN connections c     ON c.id = m.source_id
		  WHERE m.source_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+`
		  ORDER BY r.sync_def_id, r.started_at DESC`,
		sourceID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	if err != nil {
		return nil, err
	}
	defer statusRows.Close()
	for statusRows.Next() {
		var (
			defID  uuid.UUID
			status string
			errMsg *string
		)
		if err := statusRows.Scan(&defID, &status, &errMsg); err != nil {
			return nil, err
		}
		summary := usage[defID]
		s := models.MediaSetSyncRunStatus(status)
		summary.LastStatus = &s
		summary.LastErrorMessage = errMsg
		usage[defID] = summary
	}
	return usage, statusRows.Err()
}

// CheckSourceRoleForMediaSetSync verifies the actor has the required role on
// the source that owns the given media set sync.
func (r *Repo) CheckSourceRoleForMediaSetSync(ctx context.Context, syncID uuid.UUID, actorID uuid.UUID, role models.SourcePermissionRole) (bool, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM media_set_syncs m
		     JOIN connections c ON c.id = m.source_id
		    WHERE m.id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+`
		 )`,
		syncID, actorID, actorID.String(), sourceAccessRoleNames(role))
	var allowed bool
	if err := row.Scan(&allowed); err != nil {
		return false, err
	}
	return allowed, nil
}

func scanMediaSetSyncRun(r rowLikeT) (*models.MediaSetSyncRun, error) {
	v := &models.MediaSetSyncRun{}
	var (
		status        string
		selectedPaths []byte
		mismatches    []byte
	)
	if err := r.Scan(&v.ID, &v.SyncDefID, &status, &v.StartedAt, &v.FinishedAt,
		&v.AcceptedFiles, &v.SkippedFiles, &v.SchemaMismatched, &v.DispatchedFiles, &v.DispatchErrors,
		&v.BytesAccepted, &selectedPaths, &mismatches, &v.ErrorMessage, &v.TriggeredBy); err != nil {
		return nil, err
	}
	v.Status = models.MediaSetSyncRunStatus(status)
	if len(selectedPaths) > 0 && string(selectedPaths) != "null" {
		_ = json.Unmarshal(selectedPaths, &v.SelectedPaths)
	}
	if v.SelectedPaths == nil {
		v.SelectedPaths = []string{}
	}
	if len(mismatches) > 0 && string(mismatches) != "null" {
		_ = json.Unmarshal(mismatches, &v.SchemaMismatches)
	}
	if v.SchemaMismatches == nil {
		v.SchemaMismatches = []string{}
	}
	return v, nil
}

// SDC.47 — Dead-letter sinks and quarantined record storage. The repo
// methods keep ownership scoped by the sync's source via the same
// sourceAccessSQL helper used elsewhere.

func (r *Repo) CheckSourceRoleForSyncDef(ctx context.Context, syncDefID uuid.UUID, actorID uuid.UUID, role models.SourcePermissionRole) (bool, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM batch_sync_defs d
		     JOIN connections c ON c.id = d.source_id
		    WHERE d.id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4")+`
		 )`,
		syncDefID, actorID, actorID.String(), sourceAccessRoleNames(role))
	var allowed bool
	if err := row.Scan(&allowed); err != nil {
		return false, err
	}
	return allowed, nil
}

const deadLetterSinkSelect = `SELECT s.sync_def_id, s.kind, s.target_rid, s.retention_days,
	s.redaction_rules, s.updated_by, s.created_at, s.updated_at
	FROM sync_dead_letter_sinks s
	JOIN batch_sync_defs d ON d.id = s.sync_def_id
	JOIN connections c     ON c.id = d.source_id`

func (r *Repo) GetDeadLetterSink(ctx context.Context, syncDefID uuid.UUID, ownerID uuid.UUID) (*models.DeadLetterSink, error) {
	row := r.Pool.QueryRow(ctx,
		deadLetterSinkSelect+` WHERE s.sync_def_id = $1 AND `+sourceAccessSQL("c", "$2", "$3", "$4"),
		syncDefID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView))
	return scanDeadLetterSink(row)
}

func (r *Repo) UpsertDeadLetterSink(ctx context.Context, syncDefID uuid.UUID, ownerID uuid.UUID, actorID *string, req models.UpdateDeadLetterSinkRequest) (*models.DeadLetterSink, error) {
	allowed, err := r.CheckSourceRoleForSyncDef(ctx, syncDefID, ownerID, models.SourceRoleEdit)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, nil
	}
	rules := req.RedactionRules
	if rules == nil {
		rules = []models.DeadLetterRedactionRule{}
	}
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return nil, fmt.Errorf("encode redaction rules: %w", err)
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO sync_dead_letter_sinks (sync_def_id, kind, target_rid, retention_days, redaction_rules, updated_by, updated_at)
		      VALUES ($1, $2, $3, $4, $5::jsonb, $6, NOW())
		 ON CONFLICT (sync_def_id) DO UPDATE
		    SET kind            = EXCLUDED.kind,
		        target_rid      = EXCLUDED.target_rid,
		        retention_days  = EXCLUDED.retention_days,
		        redaction_rules = EXCLUDED.redaction_rules,
		        updated_by      = EXCLUDED.updated_by,
		        updated_at      = NOW()
		 RETURNING sync_def_id, kind, target_rid, retention_days, redaction_rules, updated_by, created_at, updated_at`,
		syncDefID, string(req.Kind), strings.TrimSpace(req.TargetRID), req.RetentionDays, rulesJSON, actorID)
	return scanDeadLetterSink(row)
}

func scanDeadLetterSink(r rowLikeT) (*models.DeadLetterSink, error) {
	v := &models.DeadLetterSink{}
	var (
		kind      string
		rules     []byte
		updatedBy *string
	)
	if err := r.Scan(&v.SyncDefID, &kind, &v.TargetRID, &v.RetentionDays, &rules, &updatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	v.Kind = models.DeadLetterSinkKind(kind)
	v.UpdatedBy = updatedBy
	v.RedactionRules = []models.DeadLetterRedactionRule{}
	if len(rules) > 0 && string(rules) != "null" {
		if err := json.Unmarshal(rules, &v.RedactionRules); err != nil {
			return nil, fmt.Errorf("decode redaction rules: %w", err)
		}
	}
	return v, nil
}

const quarantinedRecordSelect = `SELECT q.id, q.sync_def_id, q.run_id, q.failure_category, q.error_message,
	q.record_key, q.redacted_payload, q.redacted_headers, q.recorded_at, q.expires_at,
	q.replay_requested_at, q.replay_requested_by
	FROM quarantined_records q
	JOIN batch_sync_defs d ON d.id = q.sync_def_id
	JOIN connections c     ON c.id = d.source_id`

func (r *Repo) RecordQuarantinedRecord(ctx context.Context, syncDefID uuid.UUID, ownerID uuid.UUID, body models.RecordQuarantineRequest, sink models.DeadLetterSink, recordedAt time.Time) (*models.QuarantinedRecord, error) {
	allowed, err := r.CheckSourceRoleForSyncDef(ctx, syncDefID, ownerID, models.SourceRoleUse)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, nil
	}
	redactedPayload, redactedHeaders := models.ApplyDeadLetterRedaction(body.Payload, body.Headers, sink.RedactionRules)
	payloadJSON, err := json.Marshal(redactedPayload)
	if err != nil {
		return nil, fmt.Errorf("encode redacted payload: %w", err)
	}
	headersJSON, err := json.Marshal(redactedHeaders)
	if err != nil {
		return nil, fmt.Errorf("encode redacted headers: %w", err)
	}
	expiresAt := models.QuarantineExpiryFor(sink, recordedAt)
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO quarantined_records
		    (sync_def_id, run_id, failure_category, error_message, record_key,
		     redacted_payload, redacted_headers, recorded_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9)
		 RETURNING id, sync_def_id, run_id, failure_category, error_message, record_key,
		           redacted_payload, redacted_headers, recorded_at, expires_at,
		           replay_requested_at, replay_requested_by`,
		syncDefID, body.RunID, string(body.FailureCategory), body.ErrorMessage, body.RecordKey,
		payloadJSON, headersJSON, recordedAt, expiresAt)
	return scanQuarantinedRecord(row)
}

func (r *Repo) ListQuarantinedRecords(ctx context.Context, syncDefID uuid.UUID, ownerID uuid.UUID, category models.QuarantineFailureCategory, limit int) ([]models.QuarantinedRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{syncDefID, ownerID, ownerID.String(), sourceAccessRoleNames(models.SourceRoleView), limit}
	query := quarantinedRecordSelect + ` WHERE q.sync_def_id = $1 AND ` + sourceAccessSQL("c", "$2", "$3", "$4")
	if category != "" {
		query += ` AND q.failure_category = $6`
		args = append(args, string(category))
	}
	query += ` ORDER BY q.recorded_at DESC LIMIT $5`
	rows, err := r.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.QuarantinedRecord, 0)
	for rows.Next() {
		v, err := scanQuarantinedRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) MarkQuarantinedRecordsForReplay(ctx context.Context, syncDefID uuid.UUID, ownerID uuid.UUID, actorID *string, recordIDs []uuid.UUID, now time.Time) (int, error) {
	allowed, err := r.CheckSourceRoleForSyncDef(ctx, syncDefID, ownerID, models.SourceRoleUse)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, nil
	}
	if len(recordIDs) == 0 {
		return 0, nil
	}
	tag, err := r.Pool.Exec(ctx,
		`UPDATE quarantined_records
		    SET replay_requested_at = $3,
		        replay_requested_by = $4
		  WHERE sync_def_id = $1
		    AND id = ANY($2::uuid[])
		    AND expires_at > $3`,
		syncDefID, recordIDs, now, actorID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repo) PurgeExpiredQuarantinedRecords(ctx context.Context, now time.Time) (int, error) {
	tag, err := r.Pool.Exec(ctx, `DELETE FROM quarantined_records WHERE expires_at < $1`, now)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func scanQuarantinedRecord(r rowLikeT) (*models.QuarantinedRecord, error) {
	v := &models.QuarantinedRecord{}
	var (
		category  string
		runID     *uuid.UUID
		recordKey *string
		payload   []byte
		headers   []byte
		replayAt  *time.Time
		replayBy  *string
	)
	if err := r.Scan(&v.ID, &v.SyncDefID, &runID, &category, &v.ErrorMessage, &recordKey,
		&payload, &headers, &v.RecordedAt, &v.ExpiresAt, &replayAt, &replayBy); err != nil {
		return nil, err
	}
	v.RunID = runID
	v.FailureCategory = models.QuarantineFailureCategory(category)
	v.RecordKey = recordKey
	v.ReplayRequestedAt = replayAt
	v.ReplayRequestedBy = replayBy
	v.RedactedPayload = map[string]any{}
	v.RedactedHeaders = map[string]any{}
	if len(payload) > 0 && string(payload) != "null" {
		_ = json.Unmarshal(payload, &v.RedactedPayload)
	}
	if len(headers) > 0 && string(headers) != "null" {
		_ = json.Unmarshal(headers, &v.RedactedHeaders)
	}
	return v, nil
}
