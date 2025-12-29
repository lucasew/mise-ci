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

-- name: UpsertIssue :exec
INSERT INTO sarif_issues (id, rule_id, message, severity, tool)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING;

-- name: CreateOccurrence :exec
INSERT INTO sarif_occurrences (issue_id, run_id, path, line)
VALUES (?, ?, ?, ?);

-- name: ListSarifIssuesForRun :many
SELECT i.rule_id, i.message, o.path, o.line, i.severity, i.tool
FROM sarif_occurrences o
JOIN sarif_issues i ON o.issue_id = i.id
WHERE o.run_id = ?;

-- name: ListSarifIssuesForRepo :many
SELECT i.rule_id, i.message, o.path, o.line, i.severity, i.tool, runs.id as run_id, runs.commit_message
FROM sarif_occurrences o
JOIN sarif_issues i ON o.issue_id = i.id
JOIN runs ON o.run_id = runs.id
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
