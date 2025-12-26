-- name: CreateRun :exec
INSERT INTO runs (id, status, started_at, finished_at, exit_code, ui_token, git_link, repo_id, commit_message, author, branch)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: GetRun :one
SELECT r.id, r.status, r.started_at, r.finished_at, r.exit_code, r.ui_token, r.git_link, r.repo_id, r.commit_message, r.author, r.branch,
       re.clone_url
FROM runs r
JOIN repos re ON r.repo_id = re.id
WHERE r.id = $1;

-- name: UpdateRunStatus :exec
UPDATE runs
SET status = $2, exit_code = $3, finished_at = $4, updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: ListRuns :many
SELECT r.id, r.status, r.started_at, r.finished_at, r.exit_code, r.ui_token, r.git_link, r.repo_id, r.commit_message, r.author, r.branch,
       re.clone_url
FROM runs r
JOIN repos re ON r.repo_id = re.id
ORDER BY r.started_at DESC;

-- name: CreateRepo :exec
INSERT INTO repos (id, clone_url)
VALUES ($1, $2);

-- name: GetRepo :one
SELECT id, clone_url
FROM repos
WHERE clone_url = $1;

-- name: AppendLog :exec
INSERT INTO log_entries (run_id, timestamp, stream, data)
VALUES ($1, $2, $3, $4);

-- name: GetLogs :many
SELECT timestamp, stream, data
FROM log_entries
WHERE run_id = $1
ORDER BY id ASC;
