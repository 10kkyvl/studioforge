-- Project memory management metadata and audit trail.
ALTER TABLE memory_entries ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0 CHECK(pinned IN (0, 1));
CREATE INDEX IF NOT EXISTS idx_memory_project_pinned ON memory_entries(project_id, pinned DESC, created_at DESC);

CREATE TABLE IF NOT EXISTS memory_injections (
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  memory_entry_id TEXT NOT NULL REFERENCES memory_entries(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  PRIMARY KEY(run_id, memory_entry_id)
);
CREATE INDEX IF NOT EXISTS idx_memory_injections_entry ON memory_injections(memory_entry_id, run_id);
