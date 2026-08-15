ALTER TABLE project_agents ADD COLUMN network_policy TEXT NOT NULL DEFAULT 'unrestricted'
  CHECK (network_policy IN ('unrestricted','registry-only','none'));
ALTER TABLE runs ADD COLUMN network_policy TEXT NOT NULL DEFAULT '';
