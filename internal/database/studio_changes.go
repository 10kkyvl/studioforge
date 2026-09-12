package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/10kkyvl/studioforge/internal/security"
	"github.com/10kkyvl/studioforge/internal/studiochanges"
)

type RunChangeRecorder struct {
	store *Store
	runID string
}

func (s *Store) StudioRecorder(runID string) *RunChangeRecorder {
	return &RunChangeRecorder{store: s, runID: runID}
}

// OpenStudioJournal opens only an existing database, without running migrations
// or FTS setup in an agent subprocess. The parent has already initialized it.
func OpenStudioJournal(ctx context.Context, path, runID string) (*RunChangeRecorder, func() error, error) {
	if path == "" || runID == "" {
		return nil, nil, errors.New("Studio journal requires a database and run ID")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, nil, err
	}
	sqldb, err := sql.Open("sqlite", sqliteDSN(path)+"&mode=rw")
	if err != nil {
		return nil, nil, err
	}
	sqldb.SetMaxOpenConns(1)
	store := NewStore(&DB{SQL: sqldb, Path: path})
	if _, err := store.Run(ctx, runID); err != nil {
		_ = sqldb.Close()
		return nil, nil, fmt.Errorf("Studio journal run: %w", err)
	}
	return store.StudioRecorder(runID), sqldb.Close, nil
}

func (r *RunChangeRecorder) Start(ctx context.Context, tool string, args map[string]any) (string, error) {
	changes := studiochanges.Normalize(tool, args)
	if len(changes) == 0 {
		return "", nil
	}
	callID, now := NewID(), Now()
	tx, err := r.store.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	for _, c := range changes {
		c.Tool = security.Redact(c.Tool)
		c.Target = security.Redact(c.Target)
		for i := range c.Properties {
			c.Properties[i] = security.Redact(c.Properties[i])
		}
		props, err := json.Marshal(c.Properties)
		if err != nil {
			return "", err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO studio_changes(id,call_id,run_id,created_at,tool,target,operation,properties,status) VALUES(?,?,?,?,?,?,?,?,?)`, NewID(), callID, r.runID, now, c.Tool, c.Target, c.Operation, string(props), "pending")
		if err != nil {
			return "", err
		}
	}
	return callID, tx.Commit()
}

func (r *RunChangeRecorder) Finish(ctx context.Context, callID, status string) error {
	res, err := r.store.db.SQL.ExecContext(ctx, `UPDATE studio_changes SET status=? WHERE call_id=? AND run_id=?`, status, callID, r.runID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (s *Store) StudioChanges(ctx context.Context, runID string) ([]studiochanges.Change, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,call_id,run_id,created_at,tool,target,operation,properties,status FROM studio_changes WHERE run_id=? ORDER BY created_at,id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []studiochanges.Change{}
	for rows.Next() {
		var c studiochanges.Change
		var props string
		if err := rows.Scan(&c.ID, &c.CallID, &c.RunID, &c.CreatedAt, &c.Tool, &c.Target, &c.Operation, &props, &c.Status); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(props), &c.Properties); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
