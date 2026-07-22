package database

import (
	"context"
	"encoding/json"

	"github.com/10kkyvl/studioforge/internal/models"
)

func (s *Store) AppendConversationMessage(ctx context.Context, m models.ConversationMessage) error {
	attachments := m.Attachments
	if attachments == nil {
		attachments = []string{}
	}
	_, err := s.db.SQL.ExecContext(ctx, `INSERT INTO openrouter_messages
(thread_id,run_id,role,content,tool_calls,tool_call_id,attachments,model,input_tokens,output_tokens,cache_read_tokens,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ThreadID, nullText(m.RunID), m.Role, m.Content, m.ToolCallsJSON, nullText(m.ToolCallID), marshal(attachments), nullText(m.Model),
		m.Usage.InputTokens, m.Usage.OutputTokens, m.Usage.CacheReadTokens, Now())
	return err
}

func (s *Store) LoadConversation(ctx context.Context, threadID string) ([]models.ConversationMessage, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,thread_id,COALESCE(run_id,''),role,COALESCE(content,''),tool_calls,COALESCE(tool_call_id,''),attachments,COALESCE(model,''),input_tokens,output_tokens,cache_read_tokens,created_at
FROM openrouter_messages WHERE thread_id=? ORDER BY id ASC`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ConversationMessage, 0)
	for rows.Next() {
		var m models.ConversationMessage
		var attachments, created string
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.RunID, &m.Role, &m.Content, &m.ToolCallsJSON, &m.ToolCallID, &attachments, &m.Model, &m.Usage.InputTokens, &m.Usage.OutputTokens, &m.Usage.CacheReadTokens, &created); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTime(created)
		m.Attachments = make([]string, 0)
		if attachments != "" {
			_ = json.Unmarshal([]byte(attachments), &m.Attachments)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
