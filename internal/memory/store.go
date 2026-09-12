package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
)

type Entry struct {
	ID, ProjectID, RunID, AgentID, TaskID, Scope, Content, Summary, Source string
	Confidence, Importance                                                 float64
	CreatedAt                                                              time.Time
	Pinned, Injected                                                       bool
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
	_, err = tx.ExecContext(ctx, `INSERT INTO memory_entries(id,project_id,run_id,agent_id,task_id,scope,content,summary,source,confidence,importance,created_at,pinned) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.ID, e.ProjectID, nullText(e.RunID), nullText(e.AgentID), nullText(e.TaskID), e.Scope, e.Content, e.Summary, e.Source, e.Confidence, e.Importance, e.CreatedAt.Format(time.RFC3339Nano), boolInt(e.Pinned))
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
	pinnedRows, err := s.db.SQL.QueryContext(ctx, `SELECT id,project_id,COALESCE(run_id,''),COALESCE(agent_id,''),COALESCE(task_id,''),scope,content,summary,source,confidence,importance,created_at,pinned,0 FROM memory_entries WHERE project_id=? AND pinned=1 ORDER BY importance DESC,created_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return s.searchLike(ctx, projectID, query, limit)
	}
	pinned, err := scan(pinnedRows)
	if err != nil {
		return nil, err
	}
	if len(pinned) >= limit {
		return pinned[:limit], nil
	}
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT m.id,m.project_id,COALESCE(m.run_id,''),COALESCE(m.agent_id,''),COALESCE(m.task_id,''),m.scope,m.content,m.summary,m.source,m.confidence,m.importance,m.created_at,m.pinned,0 FROM memory_fts f JOIN memory_entries m ON m.id=f.id WHERE f.project_id=? AND m.pinned=0 AND memory_fts MATCH ? ORDER BY bm25(memory_fts),m.importance DESC,m.created_at DESC LIMIT ?`, projectID, query, limit-len(pinned))
	if err != nil {
		return s.searchLike(ctx, projectID, query, limit)
	}
	matching, err := scan(rows)
	if err != nil {
		return nil, err
	}
	return append(pinned, matching...), nil
}
func (s *Store) searchLike(ctx context.Context, projectID, query string, limit int) ([]Entry, error) {
	pattern := "%" + query + "%"
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,project_id,COALESCE(run_id,''),COALESCE(agent_id,''),COALESCE(task_id,''),scope,content,summary,source,confidence,importance,created_at,pinned,0 FROM memory_entries WHERE project_id=? AND (pinned=1 OR content LIKE ? OR summary LIKE ?) ORDER BY pinned DESC,importance DESC,created_at DESC LIMIT ?`, projectID, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	return scan(rows)
}

// List returns all project memory newest first. Injected marks entries that
// were selected for the project's most recently-created run, keeping the
// operator-visible relationship between memory and agent behaviour.
func (s *Store) List(ctx context.Context, projectID string) ([]Entry, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT m.id,m.project_id,COALESCE(m.run_id,''),COALESCE(m.agent_id,''),COALESCE(m.task_id,''),m.scope,m.content,m.summary,m.source,m.confidence,m.importance,m.created_at,m.pinned,CASE WHEN EXISTS (SELECT 1 FROM memory_injections i WHERE i.memory_entry_id=m.id AND i.run_id=(SELECT id FROM runs WHERE project_id=? ORDER BY created_at DESC,id DESC LIMIT 1)) THEN 1 ELSE 0 END FROM memory_entries m WHERE m.project_id=? ORDER BY m.created_at DESC`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	return scan(rows)
}

// Update edits content and/or pin state. Pointers let PATCH distinguish an
// omitted field from an explicit false/empty value.
func (s *Store) Update(ctx context.Context, id string, content *string, pinned *bool) (Entry, error) {
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	var e Entry
	var created string
	var pin int
	if err := tx.QueryRowContext(ctx, `SELECT id,project_id,COALESCE(run_id,''),COALESCE(agent_id,''),COALESCE(task_id,''),scope,content,summary,source,confidence,importance,created_at,pinned FROM memory_entries WHERE id=?`, id).Scan(&e.ID, &e.ProjectID, &e.RunID, &e.AgentID, &e.TaskID, &e.Scope, &e.Content, &e.Summary, &e.Source, &e.Confidence, &e.Importance, &created, &pin); err != nil {
		return Entry{}, err
	}
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	e.Pinned = pin != 0
	if content != nil {
		if strings.TrimSpace(*content) == "" {
			return Entry{}, fmt.Errorf("memory content cannot be empty")
		}
		e.Content = *content
		e.Summary = summaryForContent(e.Content)
	}
	if pinned != nil {
		e.Pinned = *pinned
	}
	if _, err := tx.ExecContext(ctx, "UPDATE memory_entries SET content=?,pinned=? WHERE id=?", e.Content, boolInt(e.Pinned), id); err != nil {
		return Entry{}, err
	}
	if s.db.FTS5 && content != nil {
		if _, err := tx.ExecContext(ctx, "DELETE FROM memory_fts WHERE id=?", id); err != nil {
			return Entry{}, err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO memory_fts(id,project_id,content,summary) VALUES(?,?,?,?)", e.ID, e.ProjectID, e.Content, e.Summary); err != nil {
			return Entry{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, err
	}
	return e, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "DELETE FROM memory_entries WHERE id=?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if s.db.FTS5 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM memory_fts WHERE id=?", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Clear(ctx context.Context, projectID string) (int, error) {
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM memory_entries WHERE project_id=?", projectID)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	res, err := tx.ExecContext(ctx, "DELETE FROM memory_entries WHERE project_id=?", projectID)
	if err != nil {
		return 0, err
	}
	if s.db.FTS5 {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, "DELETE FROM memory_fts WHERE id=?", id); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Store) RecordInjection(ctx context.Context, runID string, entryIDs []string) error {
	if runID == "" || len(entryIDs) == 0 {
		return nil
	}
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range entryIDs {
		var sameProject bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM runs r JOIN memory_entries m ON m.project_id=r.project_id WHERE r.id=? AND m.id=?)`, runID, id).Scan(&sameProject); err != nil {
			return err
		}
		if !sameProject {
			return fmt.Errorf("memory injection %s does not belong to run %s", id, runID)
		}
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO memory_injections(run_id,memory_entry_id,created_at) VALUES(?,?,?)", runID, id, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func summaryForContent(content string) string {
	content = strings.TrimSpace(content)
	if newline := strings.IndexByte(content, '\n'); newline >= 0 {
		content = strings.TrimSpace(content[:newline])
	}
	if len(content) > 140 {
		content = content[:140]
	}
	return content
}
func scan(rows *sql.Rows) ([]Entry, error) {
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var created string
		var pinned, injected int
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.RunID, &e.AgentID, &e.TaskID, &e.Scope, &e.Content, &e.Summary, &e.Source, &e.Confidence, &e.Importance, &created, &pinned, &injected); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		e.Pinned, e.Injected = pinned != 0, injected != 0
		out = append(out, e)
	}
	return out, rows.Err()
}
func (e Entry) String() string { return fmt.Sprintf("[%s %.2f] %s", e.Scope, e.Importance, e.Summary) }
