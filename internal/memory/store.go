package memory

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
)

type Entry struct {
	ID, ProjectID, RunID, AgentID, TaskID, Scope, Content, Summary, Source string
	Confidence, Importance                                                 float64
	CreatedAt                                                              time.Time
}
type Store struct{ db *database.DB }

func New(db *database.DB) *Store { return &Store{db: db} }
func (s *Store) Put(ctx context.Context, e Entry) error {
	if e.ID == "" {
		e.ID = database.NewID()
	}
	if e.Scope == "" {
		e.Scope = "project"
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO memory_entries(id,project_id,run_id,agent_id,task_id,scope,content,summary,source,confidence,importance,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, e.ID, e.ProjectID, nullText(e.RunID), nullText(e.AgentID), nullText(e.TaskID), e.Scope, e.Content, e.Summary, e.Source, e.Confidence, e.Importance, e.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if s.db.FTS5 {
		if _, err = tx.ExecContext(ctx, "INSERT INTO memory_fts(id,project_id,content,summary) VALUES(?,?,?,?)", e.ID, e.ProjectID, e.Content, e.Summary); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func nullText(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func (s *Store) Search(ctx context.Context, projectID, query string, limit int) ([]Entry, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if s.db.FTS5 {
		return s.searchFTS(ctx, projectID, query, limit)
	}
	return s.searchLike(ctx, projectID, query, limit)
}
func (s *Store) searchFTS(ctx context.Context, projectID, query string, limit int) ([]Entry, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT m.id,m.project_id,COALESCE(m.run_id,''),COALESCE(m.agent_id,''),COALESCE(m.task_id,''),m.scope,m.content,m.summary,m.source,m.confidence,m.importance,m.created_at FROM memory_fts f JOIN memory_entries m ON m.id=f.id WHERE f.project_id=? AND memory_fts MATCH ? ORDER BY bm25(memory_fts),m.importance DESC LIMIT ?`, projectID, query, limit)
	if err != nil {
		return s.searchLike(ctx, projectID, query, limit)
	}
	return scan(rows)
}
func (s *Store) searchLike(ctx context.Context, projectID, query string, limit int) ([]Entry, error) {
	pattern := "%" + query + "%"
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,project_id,COALESCE(run_id,''),COALESCE(agent_id,''),COALESCE(task_id,''),scope,content,summary,source,confidence,importance,created_at FROM memory_entries WHERE project_id=? AND (content LIKE ? OR summary LIKE ?) ORDER BY importance DESC,created_at DESC LIMIT ?`, projectID, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	return scan(rows)
}
func scan(rows *sql.Rows) ([]Entry, error) {
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var created string
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.RunID, &e.AgentID, &e.TaskID, &e.Scope, &e.Content, &e.Summary, &e.Source, &e.Confidence, &e.Importance, &created); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, e)
	}
	return out, rows.Err()
}
func (e Entry) String() string { return fmt.Sprintf("[%s %.2f] %s", e.Scope, e.Importance, e.Summary) }
