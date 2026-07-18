package database

import "context"

// TypicalRunSeconds reports the project's typical run duration, averaged
// over its last ~20 completed runs. seconds and samples are both 0 when the
// project has no completed-run history.
func (s *Store) TypicalRunSeconds(ctx context.Context, projectID string) (float64, int, error) {
	var seconds float64
	var samples int
	err := s.db.SQL.QueryRowContext(ctx, `SELECT COALESCE(AVG((julianday(finished_at)-julianday(started_at))*86400),0), COUNT(*) FROM (
		SELECT started_at,finished_at FROM runs
		WHERE project_id=? AND status='completed' AND started_at IS NOT NULL AND finished_at IS NOT NULL
		ORDER BY created_at DESC LIMIT 20
	)`, projectID).Scan(&seconds, &samples)
	if err != nil {
		return 0, 0, err
	}
	return seconds, samples, nil
}
