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

-- name: GetRunsWithoutRepoURL :many
SELECT id, status, started_at, finished_at, exit_code, ui_token, git_link, repo_url, commit_message, author, branch
FROM runs
WHERE repo_url IS NULL OR repo_url = ''
LIMIT ?;

-- name: UpdateRunRepoURL :exec
UPDATE runs
SET repo_url = ?
WHERE id = ?;

-- name: GetStuckRuns :many
SELECT id, status, started_at, finished_at, exit_code, ui_token, git_link, repo_url, commit_message, author, branch
FROM runs
WHERE status IN ('scheduled', 'running')
AND started_at < ?
LIMIT ?;

-- name: CreateRepo :exec
INSERT INTO repos (clone_url)
VALUES (?);

-- name: GetRepo :one
SELECT clone_url
FROM repos
WHERE clone_url = ?;

-- name: CheckRepoExists :one
SELECT 1 FROM repos WHERE clone_url = ? LIMIT 1;

-- name: AppendLog :exec
INSERT INTO log_entries (run_id, timestamp, stream, data)
VALUES (?, ?, ?, ?);

-- name: GetLogs :many
SELECT timestamp, stream, data
FROM log_entries
WHERE run_id = ?
ORDER BY id ASC;
