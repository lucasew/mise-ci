DROP TABLE IF EXISTS sarif_issues;
DROP TABLE IF EXISTS sarif_runs;

CREATE TABLE issues (
    id TEXT PRIMARY KEY, -- fingerprint (hash of rule_id + message + tool)
    rule_id TEXT NOT NULL,
    message TEXT NOT NULL,
    severity TEXT NOT NULL,
    tool TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(rule_id, message, tool)
);

CREATE TABLE issue_occurrences (
    id SERIAL PRIMARY KEY,
    issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    line INTEGER
);

CREATE INDEX idx_issue_occurrences_run_id ON issue_occurrences(run_id);
CREATE INDEX idx_issue_occurrences_issue_id ON issue_occurrences(issue_id);
