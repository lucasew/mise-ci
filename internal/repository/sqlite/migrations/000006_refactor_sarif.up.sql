DROP TABLE IF EXISTS sarif_issues;
DROP TABLE IF EXISTS sarif_runs;

CREATE TABLE sarif_rules (
    id TEXT PRIMARY KEY, -- deterministic hash of rule_id + tool
    rule_id TEXT NOT NULL,
    tool TEXT NOT NULL,
    severity TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(rule_id, tool)
);

CREATE TABLE sarif_findings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    rule_ref TEXT NOT NULL REFERENCES sarif_rules(id) ON DELETE CASCADE,
    message TEXT NOT NULL,
    path TEXT NOT NULL,
    line INTEGER
);

CREATE INDEX idx_sarif_findings_run_id ON sarif_findings(run_id);
CREATE INDEX idx_sarif_findings_rule_ref ON sarif_findings(rule_ref);
