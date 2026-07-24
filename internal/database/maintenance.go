package database

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const (
	eventPruneBatchSize    = 500
	eventPruneBatchPause   = 25 * time.Millisecond
	eventPruneStartupDelay = 2 * time.Minute
	eventPruneInterval     = 12 * time.Hour
)

var terminalRunStatuses = []string{"completed", "failed", "cancelled"}

func (s *Store) PruneEvents(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format(time.RFC3339Nano)
	placeholders := "?"
	for i := 1; i < len(terminalRunStatuses); i++ {
		placeholders += ",?"
	}
	query := `DELETE FROM run_events WHERE id IN (
		SELECT re.id FROM run_events re
		JOIN runs r ON r.id = re.run_id
		WHERE r.status IN (` + placeholders + `)
		  AND r.finished_at IS NOT NULL AND r.finished_at < ?
		  AND NOT (re.event_type = 'message' AND re.raw_type NOT LIKE '%.message.partial')
		LIMIT ?
	)`
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		args := make([]any, 0, len(terminalRunStatuses)+2)
		for _, status := range terminalRunStatuses {
			args = append(args, status)
		}
		args = append(args, cutoff, eventPruneBatchSize)
		res, err := s.db.SQL.ExecContext(ctx, query, args...)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < eventPruneBatchSize {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(eventPruneBatchPause):
		}
	}
}

func (s *Store) RunEventRetentionLoop(ctx context.Context, retentionDays func() int) {
	select {
	case <-time.After(eventPruneStartupDelay):
	case <-ctx.Done():
		return
	}
	s.pruneEventsOnce(ctx, retentionDays)
	ticker := time.NewTicker(eventPruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pruneEventsOnce(ctx, retentionDays)
		}
	}
}

func (s *Store) pruneEventsOnce(ctx context.Context, retentionDays func() int) {
	days := retentionDays()
	if days <= 0 {
		return
	}
	start := time.Now()
	deleted, err := s.PruneEvents(ctx, days)
	duration := time.Since(start)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			slog.Info("event retention prune interrupted", "deleted", deleted, "duration_ms", duration.Milliseconds())
			return
		}
		slog.Error("event retention prune failed", "deleted", deleted, "duration_ms", duration.Milliseconds(), "error", err)
		return
	}
	slog.Info("event retention prune complete", "deleted", deleted, "retention_days", days, "duration_ms", duration.Milliseconds())
}
