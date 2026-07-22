package openrouter

import (
	"encoding/json"

	"github.com/10kkyvl/studioforge/internal/providers/openrouter/orclient"
)

const unavailableToolResult = "[tool result unavailable: the previous run was interrupted]"

func sanitizeHistory(msgs []orclient.Message) []orclient.Message {
	out := make([]orclient.Message, 0, len(msgs))
	pending := map[string]bool{}
	var pendingOrder []string

	flushPending := func() {
		for _, id := range pendingOrder {
			out = append(out, orclient.Message{Role: "tool", ToolCallID: id, Content: unavailableToolResult})
		}
		pendingOrder = nil
		pending = map[string]bool{}
	}

	for _, m := range msgs {
		switch m.Role {
		case "assistant":
			flushPending()
			out = append(out, m)
			for _, tc := range m.ToolCalls {
				if tc.ID == "" || pending[tc.ID] {
					continue
				}
				pending[tc.ID] = true
				pendingOrder = append(pendingOrder, tc.ID)
			}
		case "tool":
			if !pending[m.ToolCallID] {
				continue
			}
			delete(pending, m.ToolCallID)
			for i, id := range pendingOrder {
				if id == m.ToolCallID {
					pendingOrder = append(pendingOrder[:i], pendingOrder[i+1:]...)
					break
				}
			}
			out = append(out, m)
		default:
			flushPending()
			out = append(out, m)
		}
	}
	flushPending()
	return out
}

const defaultMaxHistoryChars = 300000

var maxHistoryChars = defaultMaxHistoryChars

func SetMaxHistoryChars(n int) {
	if n <= 0 {
		maxHistoryChars = defaultMaxHistoryChars
		return
	}
	maxHistoryChars = n
}

const historyTrimmedNotice = "[Earlier conversation history was trimmed to fit the model's context window.]"

const (
	truncatedToolResultCap    = 400
	truncatedToolResultSuffix = "…[truncated]"
)

func messageChars(m orclient.Message) int {
	n := 0
	switch c := m.Content.(type) {
	case string:
		n += len(c)
	case nil:
	default:
		if b, err := json.Marshal(c); err == nil {
			n += len(b)
		}
	}
	if len(m.ToolCalls) > 0 {
		if b, err := json.Marshal(m.ToolCalls); err == nil {
			n += len(b)
		}
	}
	return n
}

func totalChars(msgs []orclient.Message) int {
	total := 0
	for _, m := range msgs {
		total += messageChars(m)
	}
	return total
}

func compactMessages(msgs []orclient.Message, maxChars int) ([]orclient.Message, bool) {
	if maxChars <= 0 {
		maxChars = defaultMaxHistoryChars
	}
	if totalChars(msgs) <= maxChars {
		return msgs, false
	}

	var systemMsgs []orclient.Message
	i := 0
	for i < len(msgs) && msgs[i].Role == "system" {
		systemMsgs = append(systemMsgs, msgs[i])
		i++
	}
	rest := msgs[i:]

	var groups [][]orclient.Message
	for _, m := range rest {
		if m.Role == "user" || len(groups) == 0 {
			groups = append(groups, []orclient.Message{m})
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], m)
	}

	budget := maxChars - totalChars(systemMsgs) - len(historyTrimmedNotice)
	if budget < 0 {
		budget = 0
	}
	var keptGroups [][]orclient.Message
	used := 0
	dropped := false
	for gi := len(groups) - 1; gi >= 0; gi-- {
		g := groups[gi]
		gc := totalChars(g)
		if len(keptGroups) == 0 {
			keptGroups = append([][]orclient.Message{g}, keptGroups...)
			used += gc
			continue
		}
		if used+gc <= budget {
			keptGroups = append([][]orclient.Message{g}, keptGroups...)
			used += gc
		} else {
			dropped = true
			break
		}
	}
	compacted := dropped

	assemble := func() []orclient.Message {
		out := append([]orclient.Message{}, systemMsgs...)
		if dropped {
			out = append(out, orclient.Message{Role: "system", Content: historyTrimmedNotice})
		}
		for _, g := range keptGroups {
			out = append(out, g...)
		}
		return out
	}

	out := assemble()
	if totalChars(out) > maxChars && len(keptGroups) > 0 {
		for gi := 0; gi < len(keptGroups); gi++ {
			for mi := range keptGroups[gi] {
				m := &keptGroups[gi][mi]
				if m.Role != "tool" {
					continue
				}
				s, ok := m.Content.(string)
				if !ok || len(s) <= truncatedToolResultCap {
					continue
				}
				m.Content = s[:truncatedToolResultCap] + truncatedToolResultSuffix
				compacted = true
			}
		}
		out = assemble()
	}

	return out, compacted
}
