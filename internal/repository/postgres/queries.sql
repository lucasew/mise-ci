-- name: CreateRun :exec
INSERT INTO runs (id, status, started_at, finished_at, exit_code, ui_token, git_link)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetRun :one
SELECT id, status, started_at, finished_at, exit_code, ui_token, git_link
FROM runs
WHERE id = $1;

-- name: UpdateRunStatus :exec
UPDATE runs
SET status = $1, exit_code = $2, finished_at = $3, updated_at = CURRENT_TIMESTAMP
WHERE id = $4;

-- name: ListRuns :many
SELECT id, status, started_at, finished_at, exit_code, ui_token, git_link
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
