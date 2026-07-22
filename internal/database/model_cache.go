package database

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) GetModelCache(ctx context.Context) ([]byte, time.Time, error) {
	var payload, fetchedAt string
	err := s.db.SQL.QueryRowContext(ctx, "SELECT payload,fetched_at FROM openrouter_model_cache WHERE id=1").Scan(&payload, &fetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	return []byte(payload), parseTime(fetchedAt), nil
}

func (s *Store) SetModelCache(ctx context.Context, payload []byte, fetchedAt time.Time) error {
	_, err := s.db.SQL.ExecContext(ctx, `INSERT INTO openrouter_model_cache(id,payload,fetched_at) VALUES(1,?,?)
ON CONFLICT(id) DO UPDATE SET payload=excluded.payload, fetched_at=excluded.fetched_at`, string(payload), formatTime(fetchedAt))
	return err
}
