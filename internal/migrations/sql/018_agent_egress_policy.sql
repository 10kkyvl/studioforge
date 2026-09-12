ALTER TABLE project_agents ADD COLUMN egress_policy TEXT NOT NULL DEFAULT 'unrestricted'
  CHECK (egress_policy IN ('unrestricted', 'registry-only', 'none'));
ALTER TABLE project_agents ADD COLUMN egress_registry_hosts TEXT NOT NULL DEFAULT '[]';
