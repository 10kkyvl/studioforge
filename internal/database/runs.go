package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

func (s *Store) CreateRun(ctx context.Context, run models.Run, idempotencyKey string) (models.Run, bool, error) {
	if idempotencyKey != "" {
		var existing string
		err := s.db.SQL.QueryRowContext(ctx, "SELECT id FROM runs WHERE project_id=? AND idempotency_key=?", run.ProjectID, idempotencyKey).Scan(&existing)
		if err == nil {
			r, e := s.Run(ctx, existing)
			return r, false, e
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return models.Run{}, false, err
		}
	}
	if run.ID == "" {
		run.ID = NewID()
	}
	if run.Status == "" {
		run.Status = "queued"
	}
	if run.Phase == "" {
		run.Phase = "queued"
	}
	now := time.Now().UTC()
	run.CreatedAt, run.UpdatedAt = now, now
	_, err := s.db.SQL.ExecContext(ctx, `INSERT INTO runs
(id,project_id,task_id,agent_id,provider,model_alias,provider_session_id,status,phase,required_resource,error,prompt_snapshot,base_commit,result_commit,cost,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,idempotency_key,thread_id,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.ProjectID, nullText(run.TaskID), run.AgentID, run.Provider, run.ModelAlias, run.ProviderSession, run.Status, run.Phase, run.RequiredResource, run.Error, run.PromptSnapshot, run.BaseCommit, run.ResultCommit, run.Cost, run.InputTokens, run.OutputTokens, run.CacheReadTokens, run.CacheCreationTokens, nullText(idempotencyKey), nullText(run.ThreadID), formatTime(now), formatTime(now))
	if err != nil {
		return models.Run{}, false, fmt.Errorf("create run: %w", err)
	}
	return run, true, nil
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func (s *Store) Run(ctx context.Context, id string) (models.Run, error) {
	row := s.db.SQL.QueryRowContext(ctx, `SELECT id,project_id,agent_id,COALESCE(task_id,''),provider,model_alias,provider_session_id,status,phase,required_resource,error,cost,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,base_commit,result_commit,COALESCE(thread_id,''),prompt_snapshot,created_at,updated_at,started_at,finished_at FROM runs WHERE id=?`, id)
	return scanRun(row)
}

type scanner interface{ Scan(...any) error }

func scanRun(row scanner) (models.Run, error) {
	var r models.Run
	var created, updated string
	var started, finished sql.NullString
	err := row.Scan(&r.ID, &r.ProjectID, &r.AgentID, &r.TaskID, &r.Provider, &r.ModelAlias, &r.ProviderSession, &r.Status, &r.Phase, &r.RequiredResource, &r.Error, &r.Cost, &r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheCreationTokens, &r.BaseCommit, &r.ResultCommit, &r.ThreadID, &r.PromptSnapshot, &created, &updated, &started, &finished)
	if err != nil {
		return r, err
	}
	r.CreatedAt = parseTime(created)
	r.UpdatedAt = parseTime(updated)
	if started.Valid {
		t := parseTime(started.String)
		r.StartedAt = &t
	}
	if finished.Valid {
		t := parseTime(finished.String)
		r.FinishedAt = &t
	}
	return r, nil
}

func (s *Store) ListRuns(ctx context.Context, projectID string, limit int) ([]models.Run, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,project_id,agent_id,COALESCE(task_id,''),provider,model_alias,provider_session_id,status,phase,required_resource,error,cost,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,base_commit,result_commit,COALESCE(thread_id,''),prompt_snapshot,created_at,updated_at,started_at,finished_at FROM runs WHERE (?='' OR project_id=?) ORDER BY created_at DESC LIMIT ?`, projectID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateRun(ctx context.Context, id, status, phase, resource, errText string) error {
	now := Now()
	var started, finished any
	if status == "running" {
		started = now
	}
	if status == "completed" || status == "failed" || status == "cancelled" || status == "interrupted" {
		finished = now
	}
	res, err := s.db.SQL.ExecContext(ctx, `UPDATE runs SET status=?,phase=?,required_resource=?,error=?,updated_at=?,started_at=COALESCE(started_at,?),finished_at=COALESCE(?,finished_at) WHERE id=?`, status, phase, resource, errText, now, started, finished, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetRunUsage records everything a finished run spent. Token counts land with
// the session and cost in one write so a run can never be seen with a cost but
// no tokens, or the other way round.
func (s *Store) SetRunUsage(ctx context.Context, id, session string, cost float64, tokens models.TokenUsage) error {
	_, err := s.db.SQL.ExecContext(ctx, "UPDATE runs SET provider_session_id=?,cost=?,input_tokens=?,output_tokens=?,cache_read_tokens=?,cache_creation_tokens=?,updated_at=? WHERE id=?", session, cost, tokens.InputTokens, tokens.OutputTokens, tokens.CacheReadTokens, tokens.CacheCreationTokens, Now(), id)
	return err
}

func (s *Store) RecoverInterrupted(ctx context.Context) (int64, error) {
	res, err := s.db.SQL.ExecContext(ctx, `UPDATE runs SET status='interrupted',phase='recovery',error='Daemon stopped while this run was active',updated_at=?,finished_at=? WHERE status IN ('starting','running','cancelling')`, Now(), Now())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) AppendEvents(ctx context.Context, events []models.RunEvent) ([]models.RunEvent, error) {
	if len(events) == 0 {
		return events, nil
	}
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO run_events(project_id,run_id,agent_id,event_type,raw_type,payload,created_at) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	for i := range events {
		if events[i].CreatedAt.IsZero() {
			events[i].CreatedAt = time.Now().UTC()
		}
		result, err := stmt.ExecContext(ctx, events[i].ProjectID, events[i].RunID, nullText(events[i].AgentID), events[i].Type, events[i].RawType, marshal(events[i].Payload), formatTime(events[i].CreatedAt))
		if err != nil {
			return nil, err
		}
		events[i].ID, err = result.LastInsertId()
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *Store) EventsAfter(ctx context.Context, after int64, projectID, runID string, limit int) ([]models.RunEvent, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,project_id,run_id,COALESCE(agent_id,''),event_type,raw_type,payload,created_at FROM run_events WHERE id>? AND (?='' OR project_id=?) AND (?='' OR run_id=?) ORDER BY id LIMIT ?`, after, projectID, projectID, runID, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RunEvent
	for rows.Next() {
		var e models.RunEvent
		var payload, created string
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.RunID, &e.AgentID, &e.Type, &e.RawType, &payload, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTime(created)
		var decoded any
		if json.Unmarshal([]byte(payload), &decoded) != nil {
			decoded = payload
		}
		e.Payload = decoded
		out = append(out, e)
	}
	return out, rows.Err()
}
