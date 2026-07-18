CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS project_groups (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  group_id TEXT REFERENCES project_groups(id) ON DELETE SET NULL,
  name TEXT NOT NULL,
  canonical_path TEXT NOT NULL UNIQUE,
  fingerprint TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  pinned INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
  archived INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
  mock INTEGER NOT NULL DEFAULT 0 CHECK (mock IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_opened_at TEXT,
  deleted_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_projects_group ON projects(group_id, archived, pinned);
CREATE INDEX IF NOT EXISTS idx_projects_updated ON projects(updated_at DESC);

CREATE TABLE IF NOT EXISTS tags (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  color TEXT NOT NULL DEFAULT '#718096'
);
CREATE TABLE IF NOT EXISTS project_tags (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY(project_id, tag_id)
);

CREATE TABLE IF NOT EXISTS project_settings (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(project_id, key)
);
CREATE TABLE IF NOT EXISTS project_documents (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  path TEXT NOT NULL,
  content_hash TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  UNIQUE(project_id, kind, path)
);

CREATE TABLE IF NOT EXISTS agent_templates (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL,
  model_alias TEXT NOT NULL,
  effort TEXT NOT NULL DEFAULT 'medium',
  system_prompt TEXT NOT NULL DEFAULT '',
  allowed_tools TEXT NOT NULL DEFAULT '[]',
  denied_tools TEXT NOT NULL DEFAULT '[]',
  permission_profile TEXT NOT NULL DEFAULT 'safe',
  max_turns INTEGER NOT NULL DEFAULT 20 CHECK(max_turns > 0),
  max_runtime_seconds INTEGER NOT NULL DEFAULT 1800 CHECK(max_runtime_seconds > 0),
  max_budget REAL NOT NULL DEFAULT 10 CHECK(max_budget >= 0),
  retry_policy TEXT NOT NULL DEFAULT '{}',
  concurrency INTEGER NOT NULL DEFAULT 1 CHECK(concurrency > 0),
  event_triggers TEXT NOT NULL DEFAULT '[]',
  output_schema TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS project_agents (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  template_id TEXT REFERENCES agent_templates(id) ON DELETE SET NULL,
  name TEXT NOT NULL,
  role TEXT NOT NULL,
  provider TEXT NOT NULL,
  model_alias TEXT NOT NULL,
  effort TEXT NOT NULL DEFAULT 'medium',
  permission_profile TEXT NOT NULL DEFAULT 'safe',
  enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
  concurrency INTEGER NOT NULL DEFAULT 1 CHECK(concurrency > 0),
  budget REAL NOT NULL DEFAULT 10 CHECK(budget >= 0),
  UNIQUE(project_id, name)
);
CREATE INDEX IF NOT EXISTS idx_agents_project ON project_agents(project_id, enabled);

CREATE TABLE IF NOT EXISTS agent_skills (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  version TEXT NOT NULL,
  instructions TEXT NOT NULL,
  checklist TEXT NOT NULL DEFAULT '[]',
  examples TEXT NOT NULL DEFAULT '[]',
  allowed_tools TEXT NOT NULL DEFAULT '[]',
  validation_rules TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS project_agent_skills (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  agent_id TEXT NOT NULL REFERENCES project_agents(id) ON DELETE CASCADE,
  skill_id TEXT NOT NULL REFERENCES agent_skills(id) ON DELETE CASCADE,
  PRIMARY KEY(project_id, agent_id, skill_id)
);

CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  acceptance_criteria TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 50 CHECK(priority BETWEEN 0 AND 100),
  status TEXT NOT NULL CHECK(status IN ('backlog','ready','blocked','running','review','completed','failed','cancelled')),
  assigned_agent_id TEXT REFERENCES project_agents(id) ON DELETE SET NULL,
  required_capabilities TEXT NOT NULL DEFAULT '[]',
  required_resources TEXT NOT NULL DEFAULT '[]',
  retry_policy TEXT NOT NULL DEFAULT '{}',
  budget REAL NOT NULL DEFAULT 0 CHECK(budget >= 0),
  estimated_complexity TEXT NOT NULL DEFAULT 'normal',
  related_files TEXT NOT NULL DEFAULT '[]',
  result TEXT NOT NULL DEFAULT '',
  handoff TEXT NOT NULL DEFAULT '{}',
  blocked_reason TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_project_status ON tasks(project_id, status, priority DESC);
CREATE TABLE IF NOT EXISTS task_dependencies (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  depends_on_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  PRIMARY KEY(task_id, depends_on_task_id),
  CHECK(task_id <> depends_on_task_id)
);

CREATE TABLE IF NOT EXISTS runs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  agent_id TEXT NOT NULL REFERENCES project_agents(id) ON DELETE RESTRICT,
  provider TEXT NOT NULL,
  model_alias TEXT NOT NULL,
  provider_session_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK(status IN ('queued','waiting_resources','starting','running','paused','waiting_decision','cancelling','completed','failed','cancelled','interrupted')),
  phase TEXT NOT NULL DEFAULT 'queued',
  required_resource TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  prompt_snapshot TEXT NOT NULL DEFAULT '',
  base_commit TEXT NOT NULL DEFAULT '',
  result_commit TEXT NOT NULL DEFAULT '',
  cost REAL NOT NULL DEFAULT 0 CHECK(cost >= 0),
  idempotency_key TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  UNIQUE(project_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_runs_project_status ON runs(project_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status, created_at);

CREATE TABLE IF NOT EXISTS run_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  agent_id TEXT REFERENCES project_agents(id) ON DELETE SET NULL,
  event_type TEXT NOT NULL,
  raw_type TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_run_events_replay ON run_events(id, project_id, run_id);
CREATE INDEX IF NOT EXISTS idx_run_events_run ON run_events(run_id, id);
CREATE TABLE IF NOT EXISTS run_artifacts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  path TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  size INTEGER NOT NULL CHECK(size >= 0),
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_messages (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  run_id TEXT REFERENCES runs(id) ON DELETE CASCADE,
  sender_agent_id TEXT NOT NULL REFERENCES project_agents(id) ON DELETE CASCADE,
  recipient_agent_id TEXT NOT NULL REFERENCES project_agents(id) ON DELETE CASCADE,
  task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  message_type TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_at TEXT NOT NULL,
  read_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_messages_recipient ON agent_messages(project_id, recipient_agent_id, read_at);

CREATE TABLE IF NOT EXISTS memory_entries (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
  agent_id TEXT REFERENCES project_agents(id) ON DELETE SET NULL,
  task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  scope TEXT NOT NULL CHECK(scope IN ('global','project','agent','task')),
  content TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL,
  confidence REAL NOT NULL DEFAULT 0.5 CHECK(confidence BETWEEN 0 AND 1),
  importance REAL NOT NULL DEFAULT 0.5 CHECK(importance BETWEEN 0 AND 1),
  created_at TEXT NOT NULL,
  expires_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_memory_project_scope ON memory_entries(project_id, scope, importance DESC, created_at DESC);

CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
  title TEXT NOT NULL,
  reason TEXT NOT NULL,
  proposed_action TEXT NOT NULL,
  risk TEXT NOT NULL,
  preview TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK(status IN ('pending','approved_once','approved_rule','rejected','edited')),
  resolution TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  resolved_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_decisions_project_status ON decisions(project_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS studio_sessions (
  id TEXT PRIMARY KEY,
  project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
  instance_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  place_id TEXT NOT NULL DEFAULT '',
  game_id TEXT NOT NULL DEFAULT '',
  active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0,1)),
  play_state TEXT NOT NULL DEFAULT 'stopped',
  capabilities TEXT NOT NULL DEFAULT '[]',
  mock INTEGER NOT NULL DEFAULT 0 CHECK(mock IN (0,1)),
  last_seen_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_studio_project ON studio_sessions(project_id, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS resource_leases (
  id TEXT PRIMARY KEY,
  project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  resource_key TEXT NOT NULL UNIQUE,
  acquired_at TEXT NOT NULL,
  heartbeat_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_leases_expiry ON resource_leases(expires_at);

CREATE TABLE IF NOT EXISTS process_records (
  id TEXT PRIMARY KEY,
  project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
  run_id TEXT REFERENCES runs(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  pid INTEGER NOT NULL,
  executable TEXT NOT NULL,
  started_at TEXT NOT NULL,
  heartbeat_at TEXT NOT NULL,
  exited_at TEXT,
  exit_code INTEGER,
  status TEXT NOT NULL CHECK(status IN ('starting','running','stopping','exited','killed','lost')),
  metadata TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_process_status ON process_records(status, heartbeat_at);

CREATE TABLE IF NOT EXISTS checkpoints (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
  commit_hash TEXT NOT NULL,
  branch TEXT NOT NULL,
  label TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_checkpoints_project ON checkpoints(project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS assets (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source TEXT NOT NULL,
  external_id TEXT NOT NULL,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('unreviewed','quarantined','needs_cleanup','approved','rejected')),
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE(project_id, source, external_id)
);
CREATE INDEX IF NOT EXISTS idx_assets_project_status ON assets(project_id, status);
CREATE TABLE IF NOT EXISTS asset_reviews (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
  verdict TEXT NOT NULL CHECK(verdict IN ('needs_cleanup','approved','rejected')),
  suspicious_scripts TEXT NOT NULL DEFAULT '[]',
  notes TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS budgets (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  scope TEXT NOT NULL CHECK(scope IN ('daily','milestone','task','model')),
  scope_key TEXT NOT NULL DEFAULT '',
  limit_amount REAL NOT NULL CHECK(limit_amount >= 0),
  warning_threshold REAL NOT NULL DEFAULT 0.8 CHECK(warning_threshold BETWEEN 0 AND 1),
  period_start TEXT NOT NULL,
  period_end TEXT,
  UNIQUE(project_id, scope, scope_key, period_start)
);
CREATE TABLE IF NOT EXISTS usage_records (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  agent_id TEXT NOT NULL REFERENCES project_agents(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  model_alias TEXT NOT NULL,
  input_tokens INTEGER NOT NULL DEFAULT 0 CHECK(input_tokens >= 0),
  output_tokens INTEGER NOT NULL DEFAULT 0 CHECK(output_tokens >= 0),
  cost REAL NOT NULL DEFAULT 0 CHECK(cost >= 0),
  recorded_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_usage_budget ON usage_records(project_id, recorded_at, model_alias);

