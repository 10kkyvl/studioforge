package openrouter

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/agenttools"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/orclient"
)

type StoredMessage struct {
	Role          string
	Content       string
	ToolCallsJSON string
	ToolCallID    string
	Attachments   []string
	Model         string
	Usage         providers.Usage
}

type ConversationStore interface {
	Load(ctx context.Context, threadID string) ([]StoredMessage, error)
	Append(ctx context.Context, threadID, runID string, msg StoredMessage) error
}

// storedToMessages rebuilds the wire-format history from what was persisted.
// A stored user message that carried Attachments has its image parts rebuilt
// from disk only when ws is given and the current model is vision-capable;
// each path is re-resolved and re-validated (buildUserMessage skips anything
// missing or invalid), never replayed as base64 from the store since only the
// path, not the image data, was ever persisted. Otherwise the stored text is
// used as-is.
func storedToMessages(stored []StoredMessage, ws *agenttools.Workspace, vision bool) []orclient.Message {
	out := make([]orclient.Message, 0, len(stored))
	for _, m := range stored {
		switch m.Role {
		case "user":
			if vision && len(m.Attachments) > 0 && ws != nil {
				if msg, err := buildUserMessage(ws, m.Content, m.Attachments, vision); err == nil {
					out = append(out, msg)
					continue
				}
			}
			out = append(out, orclient.Message{Role: "user", Content: m.Content})
		case "assistant":
			var calls []orclient.ToolCall
			if m.ToolCallsJSON != "" {
				_ = json.Unmarshal([]byte(m.ToolCallsJSON), &calls)
			}
			out = append(out, orclient.Message{Role: "assistant", Content: m.Content, ToolCalls: calls})
		case "tool":
			out = append(out, orclient.Message{Role: "tool", ToolCallID: m.ToolCallID, Content: m.Content})
		}
	}
	return out
}

func persistMessage(ctx context.Context, store ConversationStore, threadID, runID string, msg StoredMessage) {
	if store == nil || threadID == "" {
		return
	}
	if err := store.Append(ctx, threadID, runID, msg); err != nil {
		slog.Error("openrouter conversation persist failed", "thread", threadID, "run", runID, "role", msg.Role, "error", err)
	}
}
