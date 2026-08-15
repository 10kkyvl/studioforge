ALTER TABLE project_agents ADD COLUMN review_before_apply INTEGER NOT NULL DEFAULT 0
  CHECK (review_before_apply IN (0,1));

CREATE TABLE IF NOT EXISTS run_reviews (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  checkpoint_hash TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','applied','rejected','partial','expired')),
  selection TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  resolved_at TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_run_reviews_run ON run_reviews(run_id);
CREATE INDEX IF NOT EXISTS idx_run_reviews_pending ON run_reviews(status, expires_at);
