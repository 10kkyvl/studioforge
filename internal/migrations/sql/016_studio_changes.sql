CREATE TABLE studio_changes (
    id TEXT PRIMARY KEY,
    call_id TEXT NOT NULL,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    tool TEXT NOT NULL,
    target TEXT NOT NULL,
    operation TEXT NOT NULL,
    properties TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','succeeded','error','unknown'))
);
CREATE INDEX studio_changes_run ON studio_changes(run_id, created_at, id);
CREATE INDEX studio_changes_call ON studio_changes(call_id);
