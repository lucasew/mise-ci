-- internal/repository/postgres/migrations/000006_add_worker_tokens.up.sql
CREATE TABLE worker_tokens (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
