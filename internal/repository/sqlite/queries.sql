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
WHERE (sqlc.narg('repo_url') IS NULL OR repo_url = sqlc.narg('repo_url'))
ORDER BY started_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ListRepos :many
SELECT DISTINCT repo_url
FROM runs
WHERE repo_url IS NOT NULL AND repo_url != ''
ORDER BY repo_url;

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

-- name: UpsertRule :exec
INSERT INTO sarif_rules (id, rule_id, tool, severity)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING;

-- name: CreateFinding :exec
INSERT INTO sarif_findings (run_id, rule_ref, message, path, line)
VALUES (?, ?, ?, ?, ?);

-- name: ListFindingsForRun :many
SELECT r.rule_id, f.message, f.path, f.line, r.severity, r.tool
FROM sarif_findings f
JOIN sarif_rules r ON f.rule_ref = r.id
WHERE f.run_id = ?;

-- name: ListFindingsForRepo :many
SELECT r.rule_id, f.message, f.path, f.line, r.severity, r.tool, runs.id as run_id, runs.commit_message
FROM sarif_findings f
JOIN sarif_rules r ON f.rule_ref = r.id
JOIN runs ON f.run_id = runs.id
WHERE runs.repo_url = ?
ORDER BY runs.created_at DESC
LIMIT ?;

-- name: AppendLog :exec
INSERT INTO log_entries (run_id, timestamp, stream, data)
VALUES (?, ?, ?, ?);

-- name: GetLogs :many
SELECT timestamp, stream, data
FROM log_entries
WHERE run_id = ?
ORDER BY id ASC;
