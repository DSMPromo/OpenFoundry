package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// ErrResourcePoolNotFound is returned when a lookup misses. Handlers
// translate to HTTP 404.
var ErrResourcePoolNotFound = errors.New("resource pool not found")

// ErrResourcePoolNameTaken is returned when a Create / Update would
// collide with the `resource_pools.name` unique constraint. Handlers
// translate to HTTP 409.
var ErrResourcePoolNameTaken = errors.New("resource pool name is already taken")

const resourcePoolSelectColumns = `id, name, description,
    max_cpu_cores, max_memory_gb, max_concurrent_builds,
    priority, project_id, cluster_target, created_at, updated_at`

// CreateResourcePool inserts a pool row from the supplied request.
// Name + cluster_target are normalized (trimmed); description and
// nullable maxima are passed through as-is.
func (r *Repository) CreateResourcePool(ctx context.Context, req models.CreateResourcePoolRequest) (*models.ResourcePool, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("resource pool name is required")
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
	}
	cluster := "default"
	if req.ClusterTarget != nil && strings.TrimSpace(*req.ClusterTarget) != "" {
		cluster = strings.TrimSpace(*req.ClusterTarget)
	}
	priority := 100
	if req.Priority != nil {
		priority = *req.Priority
	}
	var pool models.ResourcePool
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO resource_pools
		   (name, description, max_cpu_cores, max_memory_gb,
		    max_concurrent_builds, priority, project_id, cluster_target)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING `+resourcePoolSelectColumns,
		name, description,
		req.MaxCPUCores, req.MaxMemoryGB, req.MaxConcurrentBuilds,
		priority, req.ProjectID, cluster,
	).Scan(
		&pool.ID, &pool.Name, &pool.Description,
		&pool.MaxCPUCores, &pool.MaxMemoryGB, &pool.MaxConcurrentBuilds,
		&pool.Priority, &pool.ProjectID, &pool.ClusterTarget,
		&pool.CreatedAt, &pool.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err, "resource_pools_name_key") {
			return nil, ErrResourcePoolNameTaken
		}
		return nil, err
	}
	return &pool, nil
}

// GetResourcePool returns a pool by id, or (nil, ErrResourcePoolNotFound).
func (r *Repository) GetResourcePool(ctx context.Context, id uuid.UUID) (*models.ResourcePool, error) {
	var pool models.ResourcePool
	err := r.db.QueryRow(
		ctx,
		`SELECT `+resourcePoolSelectColumns+` FROM resource_pools WHERE id = $1`,
		id,
	).Scan(
		&pool.ID, &pool.Name, &pool.Description,
		&pool.MaxCPUCores, &pool.MaxMemoryGB, &pool.MaxConcurrentBuilds,
		&pool.Priority, &pool.ProjectID, &pool.ClusterTarget,
		&pool.CreatedAt, &pool.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrResourcePoolNotFound
	}
	if err != nil {
		return nil, err
	}
	return &pool, nil
}

// ListResourcePools returns pools ordered by priority DESC, name ASC.
// Pagination follows the same shape as ListPipelines (default 50 per
// page, capped at 200).
func (r *Repository) ListResourcePools(ctx context.Context, page, perPage int64) (*models.ListResourcePoolsResponse, error) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 50
	}
	if perPage > 200 {
		perPage = 200
	}
	var total int64
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM resource_pools`).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT `+resourcePoolSelectColumns+`
		 FROM resource_pools
		 ORDER BY priority DESC, name ASC
		 LIMIT $1 OFFSET $2`,
		perPage, (page-1)*perPage,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []models.ResourcePool{}
	for rows.Next() {
		var p models.ResourcePool
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description,
			&p.MaxCPUCores, &p.MaxMemoryGB, &p.MaxConcurrentBuilds,
			&p.Priority, &p.ProjectID, &p.ClusterTarget,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &models.ListResourcePoolsResponse{
		Data:    items,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	}, nil
}

// UpdateResourcePool applies the non-nil fields of req. Clear flags
// (MaxCPUCoresClear, …) explicitly null out a previously-set cap;
// when both a *int value and the matching Clear flag are set, the
// Clear wins (mirrors the JSON 'null' intent).
func (r *Repository) UpdateResourcePool(ctx context.Context, id uuid.UUID, req models.UpdateResourcePoolRequest) (*models.ResourcePool, error) {
	current, err := r.GetResourcePool(ctx, id)
	if err != nil {
		return nil, err
	}
	name := current.Name
	if req.Name != nil {
		v := strings.TrimSpace(*req.Name)
		if v == "" {
			return nil, fmt.Errorf("resource pool name cannot be empty")
		}
		name = v
	}
	description := current.Description
	if req.Description != nil {
		description = *req.Description
	}
	maxCPU := current.MaxCPUCores
	if req.MaxCPUCoresClear {
		maxCPU = nil
	} else if req.MaxCPUCores != nil {
		maxCPU = req.MaxCPUCores
	}
	maxMem := current.MaxMemoryGB
	if req.MaxMemoryGBClear {
		maxMem = nil
	} else if req.MaxMemoryGB != nil {
		maxMem = req.MaxMemoryGB
	}
	maxConcurrent := current.MaxConcurrentBuilds
	if req.MaxConcurrentClear {
		maxConcurrent = nil
	} else if req.MaxConcurrentBuilds != nil {
		maxConcurrent = req.MaxConcurrentBuilds
	}
	priority := current.Priority
	if req.Priority != nil {
		priority = *req.Priority
	}
	cluster := current.ClusterTarget
	if req.ClusterTarget != nil {
		if v := strings.TrimSpace(*req.ClusterTarget); v != "" {
			cluster = v
		}
	}
	if _, err := r.db.Exec(
		ctx,
		`UPDATE resource_pools SET
		   name = $2, description = $3,
		   max_cpu_cores = $4, max_memory_gb = $5,
		   max_concurrent_builds = $6, priority = $7,
		   cluster_target = $8, updated_at = NOW()
		 WHERE id = $1`,
		id, name, description, maxCPU, maxMem, maxConcurrent, priority, cluster,
	); err != nil {
		if isUniqueViolation(err, "resource_pools_name_key") {
			return nil, ErrResourcePoolNameTaken
		}
		return nil, err
	}
	return r.GetResourcePool(ctx, id)
}

// DeleteResourcePool removes the pool. `pipeline_runs.resource_pool_id`
// is ON DELETE SET NULL so in-flight builds simply lose the
// assignment rather than cascade-deleting; the dispatcher falls back
// to the default pool.
func (r *Repository) DeleteResourcePool(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM resource_pools WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrResourcePoolNotFound
	}
	return nil
}

// isUniqueViolation reports whether err is a pgx unique-constraint
// failure matching the supplied constraint name.
func isUniqueViolation(err error, constraintName string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") &&
		(constraintName == "" || strings.Contains(msg, constraintName))
}
