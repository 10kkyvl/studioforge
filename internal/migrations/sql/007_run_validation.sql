ALTER TABLE runs ADD COLUMN validation TEXT NOT NULL DEFAULT 'none';
ALTER TABLE runs ADD COLUMN validation_screenshot TEXT;
ALTER TABLE runs ADD COLUMN parent_run_id TEXT;
ALTER TABLE runs ADD COLUMN correction_depth INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_runs_parent_run_id ON runs(parent_run_id);

ALTER TABLE project_agents ADD COLUMN validate_after_run INTEGER NOT NULL DEFAULT 0 CHECK (validate_after_run IN (0, 1));
ALTER TABLE project_agents ADD COLUMN max_correction_runs INTEGER NOT NULL DEFAULT 1;
