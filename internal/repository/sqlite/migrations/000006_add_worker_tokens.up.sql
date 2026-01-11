-- internal/repository/sqlite/migrations/000006_add_worker_tokens.up.sql
CREATE TABLE worker_tokens (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    expires_at TIMESTAMP,
    revoked_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
