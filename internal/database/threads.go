package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

// EnsureDefaultThread returns the project's single chat thread, creating it on
// first use. Sub-project 2 adds multiple named threads; the spine needs exactly
// one so a follow-up message has somewhere to continue.
func (s *Store) EnsureDefaultThread(ctx context.Context, projectID string) (models.ChatThread, error) {
	row := s.db.SQL.QueryRowContext(ctx, `SELECT id,project_id,title,created_at,updated_at FROM chat_threads WHERE project_id=? ORDER BY created_at LIMIT 1`, projectID)
	var t models.ChatThread
	var created, updated string
	err := row.Scan(&t.ID, &t.ProjectID, &t.Title, &created, &updated)
	if err == nil {
		t.CreatedAt, t.UpdatedAt = parseTime(created), parseTime(updated)
		return t, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return models.ChatThread{}, err
	}
	now := Now()
	t = models.ChatThread{ID: NewID(), ProjectID: projectID, Title: "Chat"}
	if _, err := s.db.SQL.ExecContext(ctx, `INSERT INTO chat_threads(id,project_id,title,created_at,updated_at) VALUES(?,?,?,?,?)`, t.ID, t.ProjectID, t.Title, now, now); err != nil {
		return models.ChatThread{}, err
	}
	t.CreatedAt, t.UpdatedAt = parseTime(now), parseTime(now)
	return t, nil
}

// LatestThreadSession is the Claude session to resume for the next message in a
// thread. It resumes when the thread's most recent run either completed
// cleanly, is waiting on the user to answer an interactive question
// (waiting_decision), or was paused mid-turn by the operator (paused) and
// recorded a session; if that run failed or was cancelled it returns empty so
// the next message starts fresh. This lets a
// thread self-heal from a dead or expired session instead of resuming it
// forever (a resume of a bad session fails, records no new session, and would
// otherwise resume the same stale id on every following message). A run
// parked in waiting_decision is really the same turn paused mid-flight, so
// the next message — whether a clicked option or free text — continues that
// session exactly like resuming a completed run does.
func (s *Store) LatestThreadSession(ctx context.Context, threadID string) (string, error) {
	var status, session string
	err := s.db.SQL.QueryRowContext(ctx, `SELECT status, COALESCE(provider_session_id,'') FROM runs WHERE thread_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, threadID).Scan(&status, &session)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if status != "completed" && status != "waiting_decision" && status != "paused" {
		return "", nil
	}
	return session, nil
}

// LatestThreadStuckState reports the same latest run LatestThreadSession just
// looked at, but its stuck-escalation bookkeeping instead of its session id:
// whether that run's own termination was a stuck escalation. This is what
// lets the next message in the thread suppress detection on "continue"
// without a new run row remembering anything itself.
func (s *Store) LatestThreadStuckState(ctx context.Context, threadID string) (escalated bool, err error) {
	var escalatedInt int
	err = s.db.SQL.QueryRowContext(ctx, `SELECT stuck_escalated FROM runs WHERE thread_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, threadID).Scan(&escalatedInt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return escalatedInt != 0, nil
}

// CreateThread starts a new named chat thread for a project. A blank title
// becomes "New chat" so the UI always has something to show.
func (s *Store) CreateThread(ctx context.Context, projectID, title string) (models.ChatThread, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "New chat"
	}
	now := Now()
	t := models.ChatThread{ID: NewID(), ProjectID: projectID, Title: title}
	if _, err := s.db.SQL.ExecContext(ctx, `INSERT INTO chat_threads(id,project_id,title,created_at,updated_at) VALUES(?,?,?,?,?)`, t.ID, t.ProjectID, t.Title, now, now); err != nil {
		return models.ChatThread{}, err
	}
	t.CreatedAt, t.UpdatedAt = parseTime(now), parseTime(now)
	return t, nil
}

// ListThreads returns a project's chat threads, most recently updated first.
// updated_at has only as much precision as the OS clock offers, so two
// threads created back-to-back in the same request can carry an identical
// timestamp; rowid breaks that tie in insertion order so "most recent" still
// means what callers expect. Each thread also carries its lifetime token
// totals — a SUM over every run tied to it, mirroring ListProjects's
// per-project sum — so the chat header can show what a conversation has spent
// without a second round trip.
func (s *Store) ListThreads(ctx context.Context, projectID string) ([]models.ChatThread, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT t.id,t.project_id,t.title,t.created_at,t.updated_at,
COALESCE((SELECT SUM(r.input_tokens) FROM runs r WHERE r.thread_id=t.id),0),
COALESCE((SELECT SUM(r.output_tokens) FROM runs r WHERE r.thread_id=t.id),0),
COALESCE((SELECT SUM(r.cache_read_tokens) FROM runs r WHERE r.thread_id=t.id),0),
COALESCE((SELECT SUM(r.cache_creation_tokens) FROM runs r WHERE r.thread_id=t.id),0)
FROM chat_threads t WHERE t.project_id=? ORDER BY t.updated_at DESC, t.rowid DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ChatThread, 0)
	for rows.Next() {
		var t models.ChatThread
		var created, updated string
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &created, &updated, &t.InputTokens, &t.OutputTokens, &t.CacheReadTokens, &t.CacheCreationTokens); err != nil {
			return nil, err
		}
		t.CreatedAt, t.UpdatedAt = parseTime(created), parseTime(updated)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ThreadByID looks up a single chat thread. sql.ErrNoRows bubbles up when the
// id doesn't exist.
func (s *Store) ThreadByID(ctx context.Context, threadID string) (models.ChatThread, error) {
	row := s.db.SQL.QueryRowContext(ctx, `SELECT id,project_id,title,created_at,updated_at FROM chat_threads WHERE id=?`, threadID)
	var t models.ChatThread
	var created, updated string
	if err := row.Scan(&t.ID, &t.ProjectID, &t.Title, &created, &updated); err != nil {
		return models.ChatThread{}, err
	}
	t.CreatedAt, t.UpdatedAt = parseTime(created), parseTime(updated)
	return t, nil
}

// ThreadMessages assembles a thread's full chat history: for each run in the
// thread, ordered by creation, the operator's prompt (if any) followed by the
// agent's text messages recorded as "message" run_events, in event order.
func (s *Store) ThreadMessages(ctx context.Context, threadID string) ([]models.ChatMessage, error) {
	// created_at ties at this OS clock's resolution are broken by rowid so
	// runs created back-to-back in the same request still read in the order
	// they were submitted (see ListThreads for the same issue).
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,status,prompt_snapshot,created_at FROM runs WHERE thread_id=? ORDER BY created_at, rowid`, threadID)
	if err != nil {
		return nil, err
	}
	type runRow struct {
		id, status, prompt string
		createdAt          time.Time
	}
	var runs []runRow
	for rows.Next() {
		var r runRow
		var created string
		if err := rows.Scan(&r.id, &r.status, &r.prompt, &created); err != nil {
			rows.Close()
			return nil, err
		}
		r.createdAt = parseTime(created)
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	out := make([]models.ChatMessage, 0)
	for _, r := range runs {
		if r.prompt != "" {
			out = append(out, models.ChatMessage{Role: "user", Text: r.prompt, At: r.createdAt, RunID: r.id})
		}
		eventRows, err := s.db.SQL.QueryContext(ctx, `SELECT payload,raw_type,created_at FROM run_events WHERE run_id=? AND event_type='message' ORDER BY id`, r.id)
		if err != nil {
			return nil, err
		}
		for eventRows.Next() {
			var payload, rawType, created string
			if err := eventRows.Scan(&payload, &rawType, &created); err != nil {
				eventRows.Close()
				return nil, err
			}
			text := agentEventText(payload)
			if text == "" {
				continue
			}
			out = append(out, models.ChatMessage{Role: "agent", Text: text, At: parseTime(created), RunID: r.id, Status: r.status, RawType: rawType})
		}
		if err := eventRows.Err(); err != nil {
			eventRows.Close()
			return nil, err
		}
		eventRows.Close()
	}
	return out, nil
}

// agentEventText extracts the human-readable text from a run_events
// "message" payload, mirroring the frontend's payloadText helper so a
// thread's history reads the same as the live SSE stream. Precedence: a
// top-level "text" string, then a top-level "message" string, then Claude's
// message.content[] entries where type=="text", joined with "\n". Returns ""
// when none of those shapes match.
func agentEventText(payload string) string {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return ""
	}
	if text, ok := decoded["text"].(string); ok && text != "" {
		return text
	}
	if text, ok := decoded["message"].(string); ok && text != "" {
		return text
	}
	message, ok := decoded["message"].(map[string]any)
	if !ok {
		return ""
	}
	content, ok := message["content"].([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range content {
		entry, ok := item.(map[string]any)
		if !ok || entry["type"] != "text" {
			continue
		}
		if text, ok := entry["text"].(string); ok && text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}
