CREATE TABLE sarif_rules (
    id TEXT PRIMARY KEY, -- deterministic hash of rule_id + tool
    rule_id TEXT NOT NULL,
    tool TEXT NOT NULL,
    severity TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(rule_id, tool)
);

CREATE TABLE sarif_findings (
    id SERIAL PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    rule_ref TEXT NOT NULL REFERENCES sarif_rules(id) ON DELETE CASCADE,
    message TEXT NOT NULL,
    path TEXT NOT NULL,
    line INTEGER,
    fingerprint TEXT -- external fingerprint specific to this finding
);

CREATE INDEX idx_sarif_findings_run_id ON sarif_findings(run_id);
CREATE INDEX idx_sarif_findings_rule_ref ON sarif_findings(rule_ref);
