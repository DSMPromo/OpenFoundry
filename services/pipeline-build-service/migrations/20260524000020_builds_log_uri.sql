-- A4.1: archived-log pointer for a finished build. The archive itself
-- is uploaded out-of-band by `internal/logs.Archiver` (S3 in production,
-- a no-op in test harnesses) when a build reaches a terminal state.
-- The column is the only thing routes / UI need to render a "Download
-- driver log" link, so it stays NULL until archival lands.
--
-- The legacy `pipeline_runs` table gets the same column for back-compat
-- with the older job_logs SSE path. Wrapped in a DO block because that
-- table is owned by pipeline-authoring-service and isn't present in
-- test harnesses that boot only this service's migrations.

ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS log_uri TEXT NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = current_schema() AND table_name = 'pipeline_runs'
    ) THEN
        ALTER TABLE pipeline_runs
            ADD COLUMN IF NOT EXISTS log_uri TEXT NULL;
    END IF;
END $$;
