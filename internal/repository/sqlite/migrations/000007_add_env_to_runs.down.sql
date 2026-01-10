-- SQLite doesn't support DROP COLUMN directly in older versions.
-- The safe way is to recreate the table.
PRAGMA foreign_keys=off;

CREATE TABLE runs_new (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('scheduled', 'dispatched', 'running', 'success', 'failure', 'error', 'skipped')),
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP,
    exit_code INTEGER,
    ui_token TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    git_link TEXT,
    commit_message TEXT,
    author TEXT,
    branch TEXT,
    repo_url TEXT REFERENCES repos(clone_url)
);

INSERT INTO runs_new (id, status, started_at, finished_at, exit_code, ui_token, created_at, updated_at, git_link, commit_message, author, branch, repo_url)
SELECT id, status, started_at, finished_at, exit_code, ui_token, created_at, updated_at, git_link, commit_message, author, branch, repo_url FROM runs;

DROP TABLE runs;

ALTER TABLE runs_new RENAME TO runs;

CREATE INDEX IF NOT EXISTS idx_runs_started_at ON runs(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
CREATE INDEX IF NOT EXISTS idx_runs_repo_url ON runs(repo_url);

PRAGMA foreign_keys=on;
