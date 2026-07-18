CREATE TABLE IF NOT EXISTS chat_threads (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title TEXT NOT NULL DEFAULT 'Chat',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_threads_project ON chat_threads(project_id, created_at);
ALTER TABLE runs ADD COLUMN thread_id TEXT REFERENCES chat_threads(id) ON DELETE SET NULL;
