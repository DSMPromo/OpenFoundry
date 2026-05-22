-- Build idempotency: maps an (auth subject, Idempotency-Key) pair to the
-- build that an earlier identical POST /api/v1/builds already created, so
-- a retried request returns that build instead of opening a new one.
CREATE TABLE IF NOT EXISTS build_idempotency (
    subject         TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    build_id        UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (subject, idempotency_key)
);
