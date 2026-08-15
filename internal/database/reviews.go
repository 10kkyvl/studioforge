package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

var ErrReviewAlreadyResolved = errors.New("database: review already resolved")

func (s *Store) CreateReview(ctx context.Context, review models.RunReview) (models.RunReview, error) {
	if review.ID == "" {
		review.ID = NewID()
	}
	if review.Status == "" {
		review.Status = "pending"
	}
	if review.CreatedAt.IsZero() {
		review.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.SQL.ExecContext(ctx, `INSERT INTO run_reviews(id,project_id,run_id,checkpoint_hash,status,selection,created_at,expires_at)
VALUES(?,?,?,?,?,?,?,?)`, review.ID, review.ProjectID, review.RunID, review.CheckpointHash, review.Status, review.Selection, formatTime(review.CreatedAt), formatTime(review.ExpiresAt))
	if err != nil {
		return models.RunReview{}, err
	}
	return review, nil
}

func (s *Store) Review(ctx context.Context, runID string) (models.RunReview, bool, error) {
	row := s.db.SQL.QueryRowContext(ctx, `SELECT id,project_id,run_id,checkpoint_hash,status,selection,created_at,expires_at,resolved_at FROM run_reviews WHERE run_id=?`, runID)
	review, err := scanRunReview(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.RunReview{}, false, nil
	}
	if err != nil {
		return models.RunReview{}, false, err
	}
	return review, true, nil
}

func (s *Store) PendingReviews(ctx context.Context) ([]models.RunReview, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,project_id,run_id,checkpoint_hash,status,selection,created_at,expires_at,resolved_at FROM run_reviews WHERE status='pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.RunReview{}
	for rows.Next() {
		review, err := scanRunReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, review)
	}
	return out, rows.Err()
}

func (s *Store) ResolveReview(ctx context.Context, id, status, selection string, resolvedAt time.Time) error {
	res, err := s.db.SQL.ExecContext(ctx, `UPDATE run_reviews SET status=?,selection=?,resolved_at=? WHERE id=? AND status='pending'`,
		status, selection, formatTime(resolvedAt), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrReviewAlreadyResolved
	}
	return nil
}

func (s *Store) ExpireReviews(ctx context.Context, now time.Time) ([]models.RunReview, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,project_id,run_id,checkpoint_hash,status,selection,created_at,expires_at,resolved_at FROM run_reviews WHERE status='pending' AND expires_at<=?`, formatTime(now))
	if err != nil {
		return nil, err
	}
	expired := []models.RunReview{}
	for rows.Next() {
		review, err := scanRunReview(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		expired = append(expired, review)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	resolvedAt := now.UTC()
	for i := range expired {
		expired[i].Status = "expired"
		expired[i].ResolvedAt = &resolvedAt
		if _, err := s.db.SQL.ExecContext(ctx, `UPDATE run_reviews SET status='expired',resolved_at=? WHERE id=? AND status='pending'`, formatTime(now), expired[i].ID); err != nil {
			return nil, err
		}
	}
	return expired, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRunReview(row rowScanner) (models.RunReview, error) {
	var review models.RunReview
	var created, expires string
	var resolved sql.NullString
	if err := row.Scan(&review.ID, &review.ProjectID, &review.RunID, &review.CheckpointHash, &review.Status, &review.Selection, &created, &expires, &resolved); err != nil {
		return models.RunReview{}, err
	}
	review.CreatedAt = parseTime(created)
	review.ExpiresAt = parseTime(expires)
	if resolved.Valid {
		t := parseTime(resolved.String)
		review.ResolvedAt = &t
	}
	return review, nil
}
