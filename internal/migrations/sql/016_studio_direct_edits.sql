-- A Studio MCP tool that mutates the open place (execute_luau, multi_edit,
-- insert_asset, and the rest MutatesPlace recognises) leaves no trace in git:
-- the change lives only in the Studio session, so a run's diff and rollback
-- both talk exclusively about the working tree and silently say nothing about
-- it. This column records that such a tool was used at all, so the run view
-- can warn the operator instead of leaving them to assume the diff is the
-- whole story.
ALTER TABLE runs ADD COLUMN studio_direct_edits INTEGER NOT NULL DEFAULT 0 CHECK (studio_direct_edits IN (0, 1));
