-- Pipelines, runs, and lineage

CREATE TABLE IF NOT EXISTS pipelines (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    owner_id    UUID NOT NULL,
    dag         JSONB NOT NULL DEFAULT '[]',
    status      TEXT NOT NULL DEFAULT 'draft',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS pipeline_runs (
    id           UUID PRIMARY KEY,
    pipeline_id  UUID NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending',
    started_by   UUID NOT NULL,
    node_results JSONB,
    error_message TEXT,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS lineage_edges (
    id                UUID PRIMARY KEY,
    source_dataset_id UUID NOT NULL,
    target_dataset_id UUID NOT NULL,
    pipeline_id       UUID REFERENCES pipelines(id) ON DELETE SET NULL,
    node_id           TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_dataset_id, target_dataset_id, pipeline_id)
);

CREATE INDEX IF NOT EXISTS idx_pipeline_runs_pipeline ON pipeline_runs(pipeline_id);

-- lineage_edges is now owned by lineage-service (see
-- services/lineage-service/internal/repo/migrations/
-- 20260517120000_openlineage_graph.sql). Its schema uses
-- src_dataset_rid / dst_dataset_rid rather than the
-- source_dataset_id / target_dataset_id this migration
-- originally created. We can't drop the CREATE TABLE above
-- without breaking pipeline-build-only deployments, but the
-- index creation below would fail on column lookup when the
-- new shape is already present. Guard both indexes on column
-- existence so the migration applies cleanly in either order.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'lineage_edges' AND column_name = 'source_dataset_id'
    ) THEN
        EXECUTE 'CREATE INDEX IF NOT EXISTS idx_lineage_source ON lineage_edges(source_dataset_id)';
        EXECUTE 'CREATE INDEX IF NOT EXISTS idx_lineage_target ON lineage_edges(target_dataset_id)';
    END IF;
END $$;
