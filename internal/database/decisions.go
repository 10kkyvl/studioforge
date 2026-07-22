package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

// CreateDecision persists a pending operator-approval gate. Its own producer
// decides Kind, Summary, Detail, and Payload's shape - this package never
// interprets Payload, only stores and returns it verbatim.
func (s *Store) CreateDecision(ctx context.Context, decision models.Decision) (models.Decision, error) {
	if decision.ID == "" {
		decision.ID = NewID()
	}
	decision.Status = "pending"
	decision.CreatedAt = time.Now().UTC()
	_, err := s.db.SQL.ExecContext(ctx, `INSERT INTO decisions(id,project_id,run_id,kind,summary,detail,payload,status,created_at)
VALUES(?,?,?,?,?,?,?,?,?)`, decision.ID, decision.ProjectID, decision.RunID, decision.Kind, decision.Summary, decision.Detail, decision.Payload, decision.Status, formatTime(decision.CreatedAt))
	if err != nil {
		return models.Decision{}, err
	}
	return decision, nil
}

// Decision looks up one decision by ID.
func (s *Store) Decision(ctx context.Context, id string) (models.Decision, error) {
	row := s.db.SQL.QueryRowContext(ctx, `SELECT id,project_id,run_id,kind,summary,detail,payload,status,created_at,resolved_at FROM decisions WHERE id=?`, id)
	var d models.Decision
	var created string
	var resolved sql.NullString
	if err := row.Scan(&d.ID, &d.ProjectID, &d.RunID, &d.Kind, &d.Summary, &d.Detail, &d.Payload, &d.Status, &created, &resolved); err != nil {
		return models.Decision{}, err
	}
	d.CreatedAt = parseTime(created)
	if resolved.Valid {
		t := parseTime(resolved.String)
		d.ResolvedAt = &t
	}
	return d, nil
}

// ListDecisions reports decisions in newest-first order, optionally filtered
// by status; an empty status returns every decision regardless of status.
func (s *Store) ListDecisions(ctx context.Context, status string) ([]models.Decision, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,project_id,run_id,kind,summary,detail,payload,status,created_at,resolved_at FROM decisions WHERE (?='' OR status=?) ORDER BY created_at DESC`, status, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Decision, 0)
	for rows.Next() {
		var d models.Decision
		var created string
		var resolved sql.NullString
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.RunID, &d.Kind, &d.Summary, &d.Detail, &d.Payload, &d.Status, &created, &resolved); err != nil {
			return nil, err
		}
		d.CreatedAt = parseTime(created)
		if resolved.Valid {
			t := parseTime(resolved.String)
			d.ResolvedAt = &t
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ResolveDecision records an operator's approve/deny choice. Resolving an
// already-resolved or nonexistent decision errors rather than silently
// succeeding twice, since a caller (the resolve endpoint) uses success here to
// decide whether it is the one that gets to act on the approval.
func (s *Store) ResolveDecision(ctx context.Context, id, status string) error {
	res, err := s.db.SQL.ExecContext(ctx, `UPDATE decisions SET status=?,resolved_at=? WHERE id=? AND status='pending'`, status, Now(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
