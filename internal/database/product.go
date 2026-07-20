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

// UpsertRealStudioSessions replaces the daemon's view of real (non-mock) open
// Studio instances with a freshly discovered one. An instance already bound to
// a project keeps that binding no matter what this call resolved for it — a
// refresh must never silently undo an operator's manual BindStudio choice, or
// override it with a different auto-match; only an instance with no existing
// binding picks up whatever project_id the caller resolved (which may itself
// be empty, leaving it unbound for a manual pick). An instance from a previous
// pass that is absent here is deleted outright: its Studio closed, so nothing
// keeps a stale row around to bind against later. Mock rows are never touched.
func (s *Store) UpsertRealStudioSessions(ctx context.Context, sessions []models.StudioSession) error {
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	existing := map[string]string{}
	rows, err := tx.QueryContext(ctx, `SELECT instance_id, COALESCE(project_id,'') FROM studio_sessions WHERE mock=0`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var instanceID, projectID string
		if err := rows.Scan(&instanceID, &projectID); err != nil {
			rows.Close()
			return err
		}
		existing[instanceID] = projectID
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	now := formatTime(time.Now())
	seen := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		seen[session.InstanceID] = true
		projectID := session.ProjectID
		if bound, ok := existing[session.InstanceID]; ok && bound != "" {
			projectID = bound
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO studio_sessions(id,project_id,instance_id,name,place_id,game_id,active,play_state,mock,last_seen_at)
VALUES(?,?,?,?,?,?,?,?,0,?)
ON CONFLICT(instance_id) DO UPDATE SET project_id=excluded.project_id,name=excluded.name,active=excluded.active,play_state=excluded.play_state,last_seen_at=excluded.last_seen_at`,
			"live-"+session.InstanceID, nullText(projectID), session.InstanceID, session.Name, session.PlaceID, session.GameID, boolInt(session.Active), session.PlayState, now); err != nil {
			return err
		}
	}

	for instanceID := range existing {
		if !seen[instanceID] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM studio_sessions WHERE instance_id=? AND mock=0`, instanceID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
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
