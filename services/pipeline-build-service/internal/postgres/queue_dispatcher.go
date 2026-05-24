package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/domain/queuedispatcher"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// ListQueuedRuns returns pipeline runs in 'queued' state, oldest
// first. Bounded by `limit`. Reads only the columns the dispatcher
// needs — never SELECT *.
func (r *Repository) ListQueuedRuns(ctx context.Context, limit int) ([]queuedispatcher.QueuedRun, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT pr.id, pr.pipeline_id, pr.resource_pool_id, p.project_id
		 FROM pipeline_runs pr
		 JOIN pipelines p ON p.id = pr.pipeline_id
		 WHERE pr.status IN ('queued', 'BUILD_QUEUED', 'pending')
		 ORDER BY pr.started_at ASC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list queued runs: %w", err)
	}
	defer rows.Close()
	out := make([]queuedispatcher.QueuedRun, 0, limit)
	for rows.Next() {
		var run queuedispatcher.QueuedRun
		if err := rows.Scan(&run.ID, &run.PipelineID, &run.ResourcePoolID, &run.ProjectID); err != nil {
			return nil, fmt.Errorf("scan queued run: %w", err)
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// ListResourcePoolsAll returns every pool. The HTTP CRUD path
// in postgres/resource_pools.go uses pagination; the dispatcher
// needs the unbounded set because it indexes by id every tick.
// Pool count is small in practice (operators carve <100), so the
// unbounded read is cheap.
func (r *Repository) ListResourcePoolsAll(ctx context.Context) ([]models.ResourcePool, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT `+resourcePoolSelectColumns+` FROM resource_pools ORDER BY priority DESC, name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}
	defer rows.Close()
	out := []models.ResourcePool{}
	for rows.Next() {
		var p models.ResourcePool
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description,
			&p.MaxCPUCores, &p.MaxMemoryGB, &p.MaxConcurrentBuilds,
			&p.Priority, &p.ProjectID, &p.ClusterTarget,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pool: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CurrentPoolUtilization counts RUNNING pipeline_runs assigned to
// the supplied pool. CPU + memory in-use are zero today because
// pipeline_runs doesn't yet carry per-run resource requests — once
// that lands, sum the corresponding columns and populate.
func (r *Repository) CurrentPoolUtilization(ctx context.Context, poolID uuid.UUID) (queuedispatcher.PoolUtilization, error) {
	var util queuedispatcher.PoolUtilization
	err := r.db.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM pipeline_runs
		 WHERE resource_pool_id = $1
		   AND status IN ('running', 'BUILD_RUNNING')`,
		poolID,
	).Scan(&util.RunningCount)
	if err != nil {
		return util, fmt.Errorf("pool utilization: %w", err)
	}
	return util, nil
}

// PromoteToRunning is the atomic CAS that flips a queued run to
// running and stamps resource_pool_id. Returns
// queuedispatcher.ErrAlreadyAdvanced when the row's status has
// already moved (concurrent dispatcher won the race).
func (r *Repository) PromoteToRunning(ctx context.Context, runID, poolID uuid.UUID) error {
	tag, err := r.db.Exec(
		ctx,
		`UPDATE pipeline_runs
		 SET status = 'running',
		     resource_pool_id = $2,
		     error_message = NULL
		 WHERE id = $1
		   AND status IN ('queued', 'BUILD_QUEUED', 'pending')`,
		runID, poolID,
	)
	if err != nil {
		return fmt.Errorf("promote to running: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return queuedispatcher.ErrAlreadyAdvanced
	}
	return nil
}

// MarkWaitingForResources stamps error_message with the supplied
// reason while leaving status='queued'. Idempotent — repeated ticks
// just overwrite the timestamp / phrasing.
func (r *Repository) MarkWaitingForResources(ctx context.Context, runID uuid.UUID, reason string) error {
	if _, err := r.db.Exec(
		ctx,
		`UPDATE pipeline_runs SET error_message = $2
		 WHERE id = $1 AND status IN ('queued', 'BUILD_QUEUED', 'pending')`,
		runID, reason,
	); err != nil {
		return fmt.Errorf("mark waiting: %w", err)
	}
	return nil
}

// Compile-time check that *Repository satisfies the dispatcher's
// Repository contract. If a method drifts, the build breaks here
// before main.go.
var _ queuedispatcher.Repository = (*Repository)(nil)

// _ keeps pgx imported even if the surface narrows later — used by
// the receiver type elsewhere in the package.
var (
	_ = pgx.ErrNoRows
	_ = errors.New
)
