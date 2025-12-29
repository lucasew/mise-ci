-- name: CreateRun :exec
INSERT INTO runs (id, status, started_at, finished_at, exit_code, ui_token, git_link, repo_url, commit_message, author, branch)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: GetRun :one
SELECT id, status, started_at, finished_at, exit_code, ui_token, git_link, repo_url, commit_message, author, branch
FROM runs
WHERE id = $1;

-- name: UpdateRunStatus :exec
UPDATE runs
SET status = $2, exit_code = $3, finished_at = $4, updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: ListRuns :many
SELECT id, status, started_at, finished_at, exit_code, ui_token, git_link, repo_url, commit_message, author, branch
FROM runs
WHERE (sqlc.narg('repo_url')::text IS NULL OR repo_url = sqlc.narg('repo_url'))
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
LIMIT $1;

-- name: UpdateRunRepoURL :exec
UPDATE runs
SET repo_url = $1
WHERE id = $2;

-- name: GetStuckRuns :many
SELECT id, status, started_at, finished_at, exit_code, ui_token, git_link, repo_url, commit_message, author, branch
FROM runs
WHERE status IN ('scheduled', 'running')
AND started_at < $1
LIMIT $2;

-- name: CreateRepo :exec
INSERT INTO repos (clone_url)
VALUES ($1);

-- name: GetRepo :one
SELECT clone_url
FROM repos
WHERE clone_url = $1;

-- name: CheckRepoExists :one
SELECT 1 FROM repos WHERE clone_url = $1 LIMIT 1;

-- name: CreateSarifRun :exec
INSERT INTO sarif_runs (id, run_id, tool)
VALUES ($1, $2, $3);

-- name: CreateSarifIssue :exec
INSERT INTO sarif_issues (sarif_run_id, rule_id, message, path, line, severity)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListSarifIssuesForRun :many
SELECT i.rule_id, i.message, i.path, i.line, i.severity, r.tool
FROM sarif_issues i
JOIN sarif_runs r ON i.sarif_run_id = r.id
WHERE r.run_id = $1;

-- name: ListSarifIssuesForRepo :many
SELECT i.rule_id, i.message, i.path, i.line, i.severity, r.tool, runs.id as run_id, runs.commit_message
FROM sarif_issues i
JOIN sarif_runs r ON i.sarif_run_id = r.id
JOIN runs ON r.run_id = runs.id
WHERE runs.repo_url = $1
ORDER BY runs.created_at DESC
LIMIT $2;

-- name: AppendLog :exec
INSERT INTO log_entries (run_id, timestamp, stream, data)
VALUES ($1, $2, $3, $4);

-- name: GetLogs :many
SELECT timestamp, stream, data
FROM log_entries
WHERE run_id = $1
ORDER BY id ASC;
