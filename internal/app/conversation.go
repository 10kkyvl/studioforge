package app

import (
	"context"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter"
)

type conversationAdapter struct{ store *database.Store }

func (a *conversationAdapter) Load(ctx context.Context, threadID string) ([]openrouter.StoredMessage, error) {
	rows, err := a.store.LoadConversation(ctx, threadID)
	if err != nil {
		return nil, err
	}
	out := make([]openrouter.StoredMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, openrouter.StoredMessage{
			Role:          r.Role,
			Content:       r.Content,
			ToolCallsJSON: r.ToolCallsJSON,
			ToolCallID:    r.ToolCallID,
			Attachments:   r.Attachments,
			Model:         r.Model,
			Usage: providers.Usage{
				InputTokens:     r.Usage.InputTokens,
				OutputTokens:    r.Usage.OutputTokens,
				CacheReadTokens: r.Usage.CacheReadTokens,
			},
		})
	}
	return out, nil
}

func (a *conversationAdapter) Append(ctx context.Context, threadID, runID string, msg openrouter.StoredMessage) error {
	return a.store.AppendConversationMessage(ctx, models.ConversationMessage{
		ThreadID:      threadID,
		RunID:         runID,
		Role:          msg.Role,
		Content:       msg.Content,
		ToolCallsJSON: msg.ToolCallsJSON,
		ToolCallID:    msg.ToolCallID,
		Attachments:   msg.Attachments,
		Model:         msg.Model,
		Usage: models.TokenUsage{
			InputTokens:     msg.Usage.InputTokens,
			OutputTokens:    msg.Usage.OutputTokens,
			CacheReadTokens: msg.Usage.CacheReadTokens,
		},
	})
}
