-- name: CreateRun :exec
INSERT INTO runs (id, status, started_at, finished_at, exit_code, ui_token)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetRun :one
SELECT id, status, started_at, finished_at, exit_code, ui_token
FROM runs
WHERE id = $1;

-- name: UpdateRunStatus :exec
UPDATE runs
SET status = $2, exit_code = $3, finished_at = $4, updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: ListRuns :many
SELECT id, status, started_at, finished_at, exit_code, ui_token
FROM runs
ORDER BY started_at DESC;

-- name: AppendLog :exec
INSERT INTO log_entries (run_id, timestamp, stream, data)
VALUES ($1, $2, $3, $4);

-- name: GetLogs :many
SELECT timestamp, stream, data
FROM log_entries
WHERE run_id = $1
ORDER BY id ASC;
