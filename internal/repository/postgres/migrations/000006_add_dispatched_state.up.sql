ALTER TABLE runs ADD CONSTRAINT runs_status_check CHECK (status IN ('scheduled', 'dispatched', 'running', 'success', 'failure', 'error', 'skipped'));
