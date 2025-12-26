-- name: CreateRun :exec
INSERT INTO runs (id, status, started_at, finished_at, exit_code, ui_token, git_link, repo_url, commit_message, author, branch)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetRun :one
SELECT id, status, started_at, finished_at, exit_code, ui_token, git_link, repo_url, commit_message, author, branch
FROM runs
WHERE id = ?;

-- name: UpdateRunStatus :exec
UPDATE runs
SET status = ?, exit_code = ?, finished_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: ListRuns :many
SELECT id, status, started_at, finished_at, exit_code, ui_token, git_link, repo_url, commit_message, author, branch
FROM runs
ORDER BY started_at DESC;

-- name: CreateRepo :exec
INSERT INTO repos (clone_url)
VALUES (?);

-- name: GetRepo :one
SELECT clone_url
FROM repos
WHERE clone_url = ?;

-- name: AppendLog :exec
INSERT INTO log_entries (run_id, timestamp, stream, data)
VALUES (?, ?, ?, ?);

-- name: GetLogs :many
SELECT timestamp, stream, data
FROM log_entries
WHERE run_id = ?
ORDER BY id ASC;
