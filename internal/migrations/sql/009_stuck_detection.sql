ALTER TABLE runs ADD COLUMN stuck_multiplier REAL NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN stuck_escalated INTEGER NOT NULL DEFAULT 0 CHECK (stuck_escalated IN (0, 1));

ALTER TABLE project_agents ADD COLUMN stuck_detection_disabled INTEGER NOT NULL DEFAULT 0 CHECK (stuck_detection_disabled IN (0, 1));
