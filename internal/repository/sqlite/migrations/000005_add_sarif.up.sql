CREATE TABLE sarif_runs (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    tool TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sarif_issues (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sarif_run_id TEXT NOT NULL REFERENCES sarif_runs(id) ON DELETE CASCADE,
    rule_id TEXT NOT NULL,
    message TEXT NOT NULL,
    path TEXT NOT NULL,
    line INTEGER,
    severity TEXT
);
