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
WHERE status IN ('scheduled', 'dispatched', 'running')
AND started_at < $1
LIMIT $2;

-- name: GetNextAvailableRun :one
SELECT id
FROM runs
WHERE status IN ('dispatched', 'scheduled', 'error')
ORDER BY
  CASE status
    WHEN 'dispatched' THEN 1
    WHEN 'scheduled' THEN 2
    WHEN 'error' THEN 3
  END
  started_at ASC
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: CreateRepo :exec
INSERT INTO repos (clone_url)
VALUES ($1);

-- name: GetRepo :one
SELECT clone_url
FROM repos
WHERE clone_url = $1;

-- name: CheckRepoExists :one
SELECT 1 FROM repos WHERE clone_url = $1 LIMIT 1;

-- name: UpsertRule :exec
INSERT INTO sarif_rules (id, rule_id, tool, severity)
VALUES ($1, $2, $3, $4)
ON CONFLICT(id) DO NOTHING;

-- name: CreateFinding :exec
INSERT INTO sarif_findings (run_id, rule_ref, message, path, line, fingerprint)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListFindingsForRun :many
SELECT r.rule_id, f.message, f.path, f.line, r.severity, r.tool, f.fingerprint
FROM sarif_findings f
JOIN sarif_rules r ON f.rule_ref = r.id
WHERE f.run_id = $1;

-- name: ListFindingsForRepo :many
SELECT r.rule_id, f.message, f.path, f.line, r.severity, r.tool, f.fingerprint, runs.id as run_id, runs.commit_message
FROM sarif_findings f
JOIN sarif_rules r ON f.rule_ref = r.id
JOIN runs ON f.run_id = runs.id
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

-- name: CreateWorkerToken :exec
INSERT INTO worker_tokens (id, name, expires_at, created_at, updated_at)
VALUES (, , , , );

-- name: GetWorkerToken :one
SELECT id, name, expires_at, revoked_at, created_at, updated_at
FROM worker_tokens
WHERE id = ;

-- name: ListWorkerTokens :many
SELECT id, name, expires_at, revoked_at, created_at, updated_at
FROM worker_tokens
ORDER BY created_at DESC;

-- name: RevokeWorkerToken :exec
UPDATE worker_tokens
SET revoked_at = , updated_at =
WHERE id = ;

-- name: DeleteWorkerToken :exec
DELETE FROM worker_tokens
WHERE id = ;
