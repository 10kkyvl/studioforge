CREATE TABLE IF NOT EXISTS openrouter_model_cache (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  payload TEXT NOT NULL,
  fetched_at TEXT NOT NULL
);
