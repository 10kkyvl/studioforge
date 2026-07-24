CREATE INDEX IF NOT EXISTS idx_runs_status_finished ON runs(status, finished_at);
