package database

import (
	"context"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

func (s *Store) CreateCheckpoint(ctx context.Context, checkpoint models.Checkpoint) error {
	if checkpoint.ID == "" {
		checkpoint.ID = NewID()
	}
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.SQL.ExecContext(ctx, `INSERT INTO checkpoints(id,project_id,run_id,commit_hash,branch,label,created_at)
VALUES(?,?,?,?,?,?,?)`, checkpoint.ID, checkpoint.ProjectID, checkpoint.RunID, checkpoint.CommitHash, checkpoint.Branch, checkpoint.Label, formatTime(checkpoint.CreatedAt))
	return err
}

func (s *Store) CheckpointForRun(ctx context.Context, runID string) (models.Checkpoint, error) {
	row := s.db.SQL.QueryRowContext(ctx, `SELECT id,project_id,COALESCE(run_id,''),commit_hash,branch,label,created_at FROM checkpoints WHERE run_id=? ORDER BY created_at DESC LIMIT 1`, runID)
	var c models.Checkpoint
	var created string
	if err := row.Scan(&c.ID, &c.ProjectID, &c.RunID, &c.CommitHash, &c.Branch, &c.Label, &created); err != nil {
		return models.Checkpoint{}, err
	}
	c.CreatedAt = parseTime(created)
	return c, nil
}
