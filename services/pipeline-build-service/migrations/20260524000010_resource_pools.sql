-- Resource pools — the unit a Foundry-style build dispatcher uses to
-- bound CPU / RAM / concurrent-build totals across one or many
-- projects. Each pool carves out a slice of the cluster's distributed
-- compute. When a build moves from QUEUED to RUNNING, the dispatcher
-- (TASKS_COMPUTE_PIPELINES.md A3) consults the pool's `max_*` limits;
-- if any one is exhausted the build stays in QUEUED with a
-- WAITING_FOR_RESOURCES reason instead.
--
-- pool_id is nullable on pipeline_runs (column added below) so a
-- build with no pool assignment falls back to the cluster default
-- limits — preserves backward compatibility for existing rows.
CREATE TABLE IF NOT EXISTS resource_pools (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                     TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    -- Hard caps. NULL means "unbounded for this dimension"; the
    -- dispatcher treats unbounded as "always admits". CPU is whole
    -- cores; memory is whole gibibytes. Both fit comfortably in
    -- INTEGER for any realistic cluster size.
    max_cpu_cores            INTEGER,
    max_memory_gb            INTEGER,
    max_concurrent_builds    INTEGER,
    -- Priority is a 0-1000 weight the dispatcher uses to break ties
    -- between pools competing for the same residual capacity. Higher
    -- = more important.
    priority                 INTEGER NOT NULL DEFAULT 100,
    -- Optional scope: when project_id is NULL the pool is
    -- cluster-wide and any build can target it. When set, only
    -- builds owned by that project may schedule against this pool.
    project_id               UUID,
    -- Cluster identifier the pool maps onto. For the Spark Operator
    -- this is typically a Kubernetes namespace; for the lightweight
    -- runtime it is unused. The dispatcher passes it through to
    -- SparkClient.ReserveCapacity in a later PR.
    cluster_target           TEXT NOT NULL DEFAULT 'default',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (name)
);

CREATE INDEX IF NOT EXISTS resource_pools_project_idx
    ON resource_pools (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS resource_pools_priority_idx
    ON resource_pools (priority DESC);

-- Per-build pool assignment. Nullable so legacy rows + builds the
-- author didn't explicitly target a pool for keep working.
ALTER TABLE pipeline_runs
    ADD COLUMN IF NOT EXISTS resource_pool_id UUID
        REFERENCES resource_pools(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS pipeline_runs_resource_pool_idx
    ON pipeline_runs (resource_pool_id) WHERE resource_pool_id IS NOT NULL;

-- Seed a single cluster-wide pool so a fresh install has somewhere
-- to dispatch into without an operator pre-creating one. The
-- dispatcher will treat 'default' as the fallback when a build does
-- not specify a pool.
INSERT INTO resource_pools (name, description, cluster_target, priority)
VALUES ('default', 'Cluster-wide default pool', 'default', 100)
ON CONFLICT (name) DO NOTHING;
