-- Per-agent opt-in review gate and expiry metadata for pending decisions.
ALTER TABLE project_agents ADD COLUMN review_before_apply INTEGER NOT NULL DEFAULT 0 CHECK (review_before_apply IN (0, 1));
ALTER TABLE decisions ADD COLUMN expires_at TEXT;
