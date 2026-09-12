package database

import (
	"context"
	"database/sql"
	"errors"
)

const (
	ProjectStyleSettingKey = "style"
	DefaultProjectStyle    = "cute"
)

// ProjectSetting reads a per-project key from project_settings. ok is false
// when the key has never been set for this project.
func (s *Store) ProjectSetting(ctx context.Context, projectID, key string) (string, bool, error) {
	var value string
	err := s.db.SQL.QueryRowContext(ctx, "SELECT value FROM project_settings WHERE project_id=? AND key=?", projectID, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// SetProjectSetting upserts a per-project key in project_settings.
func (s *Store) SetProjectSetting(ctx context.Context, projectID, key, value string) error {
	_, err := s.db.SQL.ExecContext(ctx, `INSERT INTO project_settings(project_id,key,value,updated_at) VALUES(?,?,?,?)
ON CONFLICT(project_id,key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, projectID, key, value, Now())
	return err
}

// ProjectStyle returns the selected UI style, falling back to the shipped
// default for projects registered before style packs existed.
func (s *Store) ProjectStyle(ctx context.Context, projectID string) (string, error) {
	value, ok, err := s.ProjectSetting(ctx, projectID, ProjectStyleSettingKey)
	if err != nil {
		return "", err
	}
	if !ok || value == "" {
		return DefaultProjectStyle, nil
	}
	return value, nil
}

// SetProjectStyle persists the project-scoped style selection.
func (s *Store) SetProjectStyle(ctx context.Context, projectID, style string) error {
	return s.SetProjectSetting(ctx, projectID, ProjectStyleSettingKey, style)
}
