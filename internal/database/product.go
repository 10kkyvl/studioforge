package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

func (s *Store) ListStudioSessions(ctx context.Context) ([]models.StudioSession, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,COALESCE(project_id,''),instance_id,name,place_id,game_id,active,play_state,mock,last_seen_at FROM studio_sessions ORDER BY active DESC,last_seen_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.StudioSession
	for rows.Next() {
		var v models.StudioSession
		var active, mock int
		var seen string
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.InstanceID, &v.Name, &v.PlaceID, &v.GameID, &active, &v.PlayState, &mock, &seen); err != nil {
			return nil, err
		}
		v.Active = active != 0
		v.Mock = mock != 0
		v.LastSeenAt = parseTime(seen)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) BindStudio(ctx context.Context, sessionID, projectID string) error {
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE studio_sessions SET project_id=NULL WHERE project_id=?", projectID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, "UPDATE studio_sessions SET project_id=? WHERE id=?", projectID, sessionID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) BudgetAllowed(ctx context.Context, projectID string, additional float64) (bool, float64, float64, error) {
	var limit, used float64
	err := s.db.SQL.QueryRowContext(ctx, `SELECT COALESCE((SELECT SUM(limit_amount) FROM budgets WHERE project_id=? AND scope='daily' AND period_start<=? AND (period_end IS NULL OR period_end>?)),0),COALESCE((SELECT SUM(cost) FROM usage_records WHERE project_id=? AND recorded_at>=?),0)`, projectID, Now(), Now(), projectID, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339Nano)).Scan(&limit, &used)
	if err != nil {
		return false, 0, 0, err
	}
	if limit == 0 {
		return true, limit, used, nil
	}
	return used+additional <= limit, limit, used, nil
}
