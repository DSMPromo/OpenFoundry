-- identity-federation-service slice 1 — minimal auth schema.
--
-- Subset of the Rust crate's 9 migrations needed for register/login/
-- token. Wider schema (orgs, MFA tables, SCIM external IDs, JWKS keys,
-- control panel) lands in later slices.
--
-- Sessions + refresh tokens are stored in Postgres for slice 1; the
-- Rust crate keeps them in Cassandra (`auth_runtime.*`) and slice 2
-- migrates the Go port to the same Cassandra adapter.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS users (
    id              UUID PRIMARY KEY,
    email           TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    password_hash   TEXT NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    auth_source     TEXT NOT NULL DEFAULT 'local',
    mfa_enforced    BOOLEAN NOT NULL DEFAULT false,
    organization_id UUID,
    attributes      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);

CREATE TABLE IF NOT EXISTS roles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id     UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);

-- Seed the canonical role triad. The `Register` handler looks up the
-- `admin` role by name when assigning the first-user-becomes-admin
-- promotion; without these rows the call fails with `lookup role
-- admin: no rows in result set` on a fresh install.
-- The `roles` table is shared with authorization-policy-service which
-- evolves it to tenant-aware uniqueness (partial unique index on
-- `name` WHERE tenant_id IS NULL). The ON CONFLICT target below uses
-- the inference form so Postgres binds to that partial index whether
-- the table is at slice-1 shape (plain UNIQUE on `name`) or the
-- evolved shape.
INSERT INTO roles (id, name, description) VALUES
    (gen_random_uuid(), 'admin', 'Full platform administrator'),
    (gen_random_uuid(), 'editor', 'Can create and modify resources'),
    (gen_random_uuid(), 'viewer', 'Read-only access')
ON CONFLICT (name) WHERE tenant_id IS NULL DO NOTHING;

-- Postgres-backed refresh tokens for slice 1.
-- Slice 2 migrates these to Cassandra `auth_runtime.refresh_tokens`
-- with a TTL-based GC. While both implementations coexist (Rust
-- production = Cassandra, Go slice 1 = Postgres), the Go port runs
-- against its own DATABASE_URL and does NOT need to share the table
-- with the Rust crate.
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      TEXT NOT NULL UNIQUE,
    family_id       UUID NOT NULL,
    issued_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens (user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_family ON refresh_tokens (family_id);
