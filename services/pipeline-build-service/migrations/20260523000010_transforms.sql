-- Transforms: reusable Python or SQL source bundles that pipeline
-- nodes reference by id. A transform is independent of any pipeline
-- so it can be authored, validated, and previewed before wiring it
-- into a graph; once published it is referenced via `transform_id`
-- in a node's metadata.
CREATE TABLE IF NOT EXISTS transforms (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    language    TEXT NOT NULL CHECK (language IN ('PYTHON', 'SQL')),
    source      TEXT NOT NULL,
    -- Free-form config (timeout_seconds, allowed_packages, output
    -- schema hints, …). JSONB so callers can query individual keys.
    config      JSONB NOT NULL DEFAULT '{}'::jsonb,
    owner_id    UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS transforms_owner_id_idx ON transforms(owner_id);
CREATE INDEX IF NOT EXISTS transforms_language_idx ON transforms(language);
CREATE INDEX IF NOT EXISTS transforms_created_at_idx ON transforms(created_at DESC);
