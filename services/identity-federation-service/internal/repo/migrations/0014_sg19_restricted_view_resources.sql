-- residency: us-east-1
-- SG.19 restricted-view resource CRUD.
--
-- Extends the slice-7a CBAC row with Foundry-style resource metadata:
-- backing dataset, project/folder placement, owners, policy payload,
-- assumed markings, transaction/build history, and explicit transform
-- input blocking.

ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS backing_dataset_rid TEXT NOT NULL DEFAULT '';
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS backing_dataset_branch TEXT;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS project_rid TEXT;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS folder_rid TEXT;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS path TEXT;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS owner_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS policy JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS assumed_markings JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS transactions JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS output_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS view_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS transform_input_blocked BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS last_built_at TIMESTAMPTZ;
ALTER TABLE restricted_views ADD COLUMN IF NOT EXISTS build_status TEXT NOT NULL DEFAULT 'not_built';

UPDATE restricted_views
   SET policy = conditions
 WHERE policy = '{}'::jsonb
   AND conditions <> '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_restricted_views_backing_dataset
    ON restricted_views (backing_dataset_rid);

CREATE INDEX IF NOT EXISTS idx_restricted_views_project_folder
    ON restricted_views (project_rid, folder_rid);

CREATE INDEX IF NOT EXISTS idx_restricted_views_policy
    ON restricted_views USING GIN (policy);

INSERT INTO permissions (resource, action, description) VALUES
    ('restricted_view', 'create', 'Create a restricted view resource in a folder or Project'),
    ('restricted_view', 'create_resource', 'Create restricted view resource metadata'),
    ('dataset', 'create_restricted_view', 'Create a restricted view for a backing dataset'),
    ('dataset', 'restricted_view:create', 'Create a restricted view for a backing dataset'),
    ('restricted_view_policy', 'read', 'Read a restricted view granular policy'),
    ('restricted_view_policy', 'edit', 'Edit a restricted view granular policy'),
    ('restricted_view_transaction', 'view', 'View restricted view transaction metadata'),
    ('restricted_view', 'build', 'Build or rebuild a restricted view'),
    ('restricted_view', 'read', 'Read restricted view rows and metadata'),
    ('restricted_view', 'edit', 'Edit restricted view metadata'),
    ('restricted_view', 'manage', 'Manage restricted view permissions and lifecycle')
ON CONFLICT (resource, action) WHERE tenant_id IS NULL DO UPDATE
    SET description = EXCLUDED.description;
