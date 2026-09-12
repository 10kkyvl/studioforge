package scheduler

import (
	"context"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/10kkyvl/studioforge/internal/memory"
)

// Keep a bounded extract of the final answer, without a second model request.
// This is attributed history, not a claim that every statement was verified.
func (m *Manager) rememberOutcome(j *Job, answer string) {
	if strings.TrimSpace(answer) == "" || j.Mode == "plan" {
		return
	}
	m.mu.Lock()
	mem := m.memoryStore
	m.mu.Unlock()
	if mem == nil {
		return
	}
	run, err := m.store.Run(context.Background(), j.RunID)
	if err != nil || run.Status != "completed" {
		return
	}
	if run.Validation != "" && run.Validation != "none" && run.Validation != "passed" && run.Validation != "corrected" {
		return
	}
	content := "Reported outcome: " + outcomeExcerpt(answer, 1800) + "\nTask: " + boundedMemoryText(j.Prompt, 240)
	entry := memory.Entry{ProjectID: j.ProjectID, RunID: j.RunID, AgentID: j.AgentID, TaskID: j.TaskID, Content: content, Summary: outcomeExcerpt(answer, 560), Source: "run", Confidence: 0.5, Importance: 0.5}
	if err := mem.Put(context.Background(), entry); err != nil {
		slog.Warn("failed to persist run memory", "run_id", j.RunID, "error", err)
	}
}

func boundedMemoryText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	end := limit - len("…")
	if end < 0 {
		return ""
	}
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "…"
}

// Keep the conclusion too: verification and limitations often appear at the end.
func outcomeExcerpt(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	const gap = " … "
	headBudget := (limit - len(gap)) * 2 / 3
	tailBudget := limit - len(gap) - headBudget
	head := boundedMemoryText(s, headBudget)
	start := len(s) - tailBudget
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return head + gap + s[start:]
}
