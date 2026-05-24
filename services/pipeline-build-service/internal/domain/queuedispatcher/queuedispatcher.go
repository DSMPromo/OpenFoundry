// Package queuedispatcher implements the Foundry-style build
// dispatcher described in TASKS_COMPUTE_PIPELINES.md Task A3. It
// reads QUEUED pipeline runs from `pipeline_runs`, consults each
// run's target `resource_pools` entry to decide whether the cluster
// has capacity, and either promotes the run to RUNNING or leaves it
// in QUEUED with a "waiting for capacity" reason.
//
// Scope of this slice (A3.2):
//
//   - In-process scheduler driven by a ticker the service main
//     starts at boot. Stateless apart from the underlying DB.
//   - Per-pool capacity check today only counts concurrent
//     RUNNING builds against `resource_pools.max_concurrent_builds`.
//     CPU + memory caps (max_cpu_cores, max_memory_gb) carry through
//     to the pool row but are NOT enforced yet — runs do not declare
//     per-run CPU/RAM requirements until the build-author surface
//     gains that field. They'll be wired once that lands.
//   - Cross-project fairness (round-robin) is implemented over the
//     queued-runs FIFO: within one tick we cycle through pools that
//     still have capacity rather than draining one pool entirely
//     before touching the next.
//   - Spark capacity reservation (SparkClient.ReserveCapacity) is
//     stubbed via the Reserver interface; the production wiring
//     happens in a follow-up PR once we settle on the right Spark
//     Operator integration shape.
package queuedispatcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// QueuedRun is the narrow projection the dispatcher needs from a
// `pipeline_runs` row in `queued` state.
type QueuedRun struct {
	ID             uuid.UUID
	PipelineID     uuid.UUID
	ResourcePoolID *uuid.UUID
	ProjectID      *uuid.UUID
}

// PoolUtilization is the current load against a pool: how many runs
// are RUNNING right now, and what CPU/memory totals they reserve.
// Returned by Repository.CurrentPoolUtilization.
type PoolUtilization struct {
	RunningCount  int
	CPUInUse      int
	MemoryGBInUse int
}

// Repository is the persistence seam the dispatcher uses. Production
// code wires the postgres Repository; tests inject a fake.
type Repository interface {
	// ListQueuedRuns returns all runs currently in 'queued' state,
	// ordered by created_at ASC (FIFO). Bounded internally so a
	// runaway queue doesn't OOM the dispatcher.
	ListQueuedRuns(ctx context.Context, limit int) ([]QueuedRun, error)

	// ListResourcePoolsAll returns every pool — small set in
	// practice (operators don't carve hundreds), no pagination needed.
	// Distinct name from the paginated REST listing so postgres
	// Repository can implement both without method collision.
	ListResourcePoolsAll(ctx context.Context) ([]models.ResourcePool, error)

	// CurrentPoolUtilization sums today's load on one pool:
	// 'running' rows assigned to it, plus their CPU/RAM if the
	// pipeline declared any (best-effort).
	CurrentPoolUtilization(ctx context.Context, poolID uuid.UUID) (PoolUtilization, error)

	// PromoteToRunning is the atomic CAS that moves a run from
	// 'queued' to 'running' and stamps resource_pool_id. Returns
	// ErrAlreadyAdvanced when the row's status has already moved
	// (another dispatcher instance won the race), so the caller
	// just continues to the next run.
	PromoteToRunning(ctx context.Context, runID, poolID uuid.UUID) error

	// MarkWaitingForResources stamps error_message with the
	// supplied reason but leaves status='queued'. Subsequent
	// ticks will re-evaluate. The dispatcher only calls this once
	// the run has gone through ≥1 tick without capacity, to keep
	// the noise down.
	MarkWaitingForResources(ctx context.Context, runID uuid.UUID, reason string) error
}

// Reserver is the optional Spark / Kubernetes hook the production
// wiring plugs in. The dispatcher calls Reserve once it has decided
// a run should promote; a non-nil error rolls the decision back
// (the run stays QUEUED until next tick).
//
// Today this interface is purely structural — the default Reserve
// is a no-op shim — but it lets A3.2 ship without blocking on the
// Spark-Operator handshake, and lets A3.x add the real reservation
// later without rewriting the dispatcher.
type Reserver interface {
	Reserve(ctx context.Context, run QueuedRun, pool models.ResourcePool) error
}

// noopReserver is the default Reserver — always admits. main.go
// can override with a real Kubernetes / Spark client.
type noopReserver struct{}

func (noopReserver) Reserve(_ context.Context, _ QueuedRun, _ models.ResourcePool) error {
	return nil
}

// ErrAlreadyAdvanced is returned by PromoteToRunning when another
// dispatcher instance (or a manual API call) has already moved the
// run out of 'queued'. The caller treats it as a no-op miss, not
// a failure.
var ErrAlreadyAdvanced = errors.New("queuedispatcher: run already advanced from queued")

// Dispatcher is the in-process scheduler.
type Dispatcher struct {
	Repo     Repository
	Reserver Reserver
	Logger   *slog.Logger
	// PerTickLimit caps how many queued runs we evaluate per Tick.
	// Defaults to 100 when zero.
	PerTickLimit int
}

// New builds a Dispatcher with the supplied repo + sane defaults.
func New(repo Repository, opts ...Option) *Dispatcher {
	d := &Dispatcher{
		Repo:         repo,
		Reserver:     noopReserver{},
		Logger:       slog.Default(),
		PerTickLimit: 100,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Option mutates a freshly-constructed Dispatcher.
type Option func(*Dispatcher)

// WithReserver swaps the noop reserver for a real one.
func WithReserver(r Reserver) Option {
	return func(d *Dispatcher) {
		if r != nil {
			d.Reserver = r
		}
	}
}

// WithLogger swaps the default slog logger.
func WithLogger(l *slog.Logger) Option {
	return func(d *Dispatcher) {
		if l != nil {
			d.Logger = l
		}
	}
}

// WithPerTickLimit caps the number of runs evaluated per Tick.
func WithPerTickLimit(n int) Option {
	return func(d *Dispatcher) {
		if n > 0 {
			d.PerTickLimit = n
		}
	}
}

// TickResult is the outcome of one Tick. Returned for observability
// + tests; future A3.3 emits Prometheus counters off these numbers.
type TickResult struct {
	Promoted   int
	Waiting    int
	NoPoolHit  int
	Errors     int
	Considered int
}

// Tick runs one pass of the dispatcher. Safe to call concurrently —
// per-run advancement is atomic via PromoteToRunning's CAS.
func (d *Dispatcher) Tick(ctx context.Context) (TickResult, error) {
	result := TickResult{}

	pools, err := d.Repo.ListResourcePoolsAll(ctx)
	if err != nil {
		return result, fmt.Errorf("queuedispatcher: list pools: %w", err)
	}
	poolByID, defaultPool := indexPools(pools)
	if defaultPool == nil && len(pools) == 0 {
		// No pools configured at all — nothing to schedule into.
		// The migration in A3.1 seeded a default pool, so this is
		// the "operator deleted every pool" edge case.
		d.Logger.WarnContext(ctx, "queuedispatcher: no resource pools configured, skipping tick")
		return result, nil
	}

	runs, err := d.Repo.ListQueuedRuns(ctx, d.PerTickLimit)
	if err != nil {
		return result, fmt.Errorf("queuedispatcher: list queued runs: %w", err)
	}
	result.Considered = len(runs)

	// Group runs by target pool so we can apply per-pool capacity
	// checks once + then drain each pool's queue. Runs without a
	// resource_pool_id fall into the default pool.
	bucket := map[uuid.UUID][]QueuedRun{}
	for _, run := range runs {
		pool := resolvePool(run, poolByID, defaultPool)
		if pool == nil {
			result.NoPoolHit++
			d.Logger.WarnContext(ctx, "queuedispatcher: queued run has no assignable pool",
				slog.String("run_id", run.ID.String()))
			continue
		}
		bucket[pool.ID] = append(bucket[pool.ID], run)
	}

	// Round-robin across pools so a single big-queue pool can't
	// starve every other pool's first run.
	queueIdx := map[uuid.UUID]int{}
	poolIDs := sortedKeys(bucket)
	for active := true; active; {
		active = false
		for _, poolID := range poolIDs {
			idx := queueIdx[poolID]
			if idx >= len(bucket[poolID]) {
				continue
			}
			active = true
			pool := poolByID[poolID]
			run := bucket[poolID][idx]
			queueIdx[poolID] = idx + 1

			util, uerr := d.Repo.CurrentPoolUtilization(ctx, pool.ID)
			if uerr != nil {
				d.Logger.ErrorContext(ctx, "queuedispatcher: utilization lookup failed",
					slog.String("pool_id", pool.ID.String()), slog.String("error", uerr.Error()))
				result.Errors++
				continue
			}
			if !poolHasCapacity(pool, util) {
				if err := d.Repo.MarkWaitingForResources(ctx, run.ID, capacityReason(pool, util)); err != nil {
					d.Logger.WarnContext(ctx, "queuedispatcher: mark waiting failed",
						slog.String("run_id", run.ID.String()), slog.String("error", err.Error()))
				}
				result.Waiting++
				continue
			}
			if err := d.Reserver.Reserve(ctx, run, *pool); err != nil {
				d.Logger.WarnContext(ctx, "queuedispatcher: reserve failed",
					slog.String("run_id", run.ID.String()), slog.String("error", err.Error()))
				result.Errors++
				continue
			}
			if err := d.Repo.PromoteToRunning(ctx, run.ID, pool.ID); err != nil {
				if errors.Is(err, ErrAlreadyAdvanced) {
					// Concurrent dispatcher / manual API call won.
					// Treat as a no-op; counted as 'promoted' from
					// the system's perspective.
					result.Promoted++
					continue
				}
				d.Logger.ErrorContext(ctx, "queuedispatcher: promote failed",
					slog.String("run_id", run.ID.String()), slog.String("error", err.Error()))
				result.Errors++
				continue
			}
			result.Promoted++
		}
	}
	return result, nil
}

// indexPools returns a name-indexed lookup plus the default pool
// (the one named 'default', if present).
func indexPools(pools []models.ResourcePool) (map[uuid.UUID]*models.ResourcePool, *models.ResourcePool) {
	out := make(map[uuid.UUID]*models.ResourcePool, len(pools))
	var defaultPool *models.ResourcePool
	for i := range pools {
		p := pools[i]
		out[p.ID] = &p
		if p.Name == "default" {
			defaultPool = &p
		}
	}
	return out, defaultPool
}

// resolvePool picks the target pool for a run: explicit assignment
// when present, falls back to the cluster-wide default.
func resolvePool(run QueuedRun, byID map[uuid.UUID]*models.ResourcePool, defaultPool *models.ResourcePool) *models.ResourcePool {
	if run.ResourcePoolID != nil {
		if p, ok := byID[*run.ResourcePoolID]; ok {
			return p
		}
	}
	return defaultPool
}

// poolHasCapacity reports whether the pool can admit one more run
// given current utilization. Only max_concurrent_builds is enforced
// today; CPU and memory caps are advisory until per-run resource
// requests land.
func poolHasCapacity(pool *models.ResourcePool, util PoolUtilization) bool {
	if pool.MaxConcurrentBuilds != nil && util.RunningCount >= *pool.MaxConcurrentBuilds {
		return false
	}
	return true
}

// capacityReason renders the human-readable wait reason persisted in
// pipeline_runs.error_message. Kept short — operators read this from
// the build inspector tooltip.
func capacityReason(pool *models.ResourcePool, util PoolUtilization) string {
	if pool.MaxConcurrentBuilds != nil {
		return fmt.Sprintf("waiting for capacity in pool %q (%d/%d concurrent builds in use)",
			pool.Name, util.RunningCount, *pool.MaxConcurrentBuilds)
	}
	return fmt.Sprintf("waiting for capacity in pool %q", pool.Name)
}

// sortedKeys returns the pool IDs in deterministic order so the
// round-robin walk is reproducible across ticks (matters for tests
// and operator audit trails alike).
func sortedKeys(m map[uuid.UUID][]QueuedRun) []uuid.UUID {
	keys := make([]uuid.UUID, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].String() < keys[j].String()
	})
	return keys
}
