-- name: CreateRun :exec
INSERT INTO runs (id, status, started_at, finished_at, exit_code, ui_token, git_link, repo_id, commit_message, author, branch)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetRun :one
SELECT r.id, r.status, r.started_at, r.finished_at, r.exit_code, r.ui_token, r.git_link, r.repo_id, r.commit_message, r.author, r.branch,
       re.clone_url
FROM runs r
JOIN repos re ON r.repo_id = re.id
WHERE r.id = ?;

-- name: UpdateRunStatus :exec
UPDATE runs
SET status = ?, exit_code = ?, finished_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: ListRuns :many
SELECT r.id, r.status, r.started_at, r.finished_at, r.exit_code, r.ui_token, r.git_link, r.repo_id, r.commit_message, r.author, r.branch,
       re.clone_url
FROM runs r
JOIN repos re ON r.repo_id = re.id
ORDER BY r.started_at DESC;

-- name: CreateRepo :exec
INSERT INTO repos (id, clone_url)
VALUES (?, ?);

-- name: GetRepo :one
SELECT id, clone_url
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
