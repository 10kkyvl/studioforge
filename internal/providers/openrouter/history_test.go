package openrouter

import (
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/providers/openrouter/orclient"
)

func containsContent(msgs []orclient.Message, substr string) bool {
	for _, m := range msgs {
		if s, ok := m.Content.(string); ok && strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

func TestCompactMessages_DropsContiguousSuffixNotGap(t *testing.T) {
	noticeLen := len(historyTrimmedNotice)
	maxChars := noticeLen + 50

	g0 := "g0-oldest-small"
	g1 := strings.Repeat("g1-large-", 25)
	g2 := "g2-newest-small"

	msgs := []orclient.Message{
		{Role: "user", Content: g0},
		{Role: "user", Content: g1},
		{Role: "user", Content: g2},
	}

	out, compacted := compactMessages(msgs, maxChars)
	if !compacted {
		t.Fatalf("expected compaction to occur")
	}
	if containsContent(out, g0) {
		t.Errorf("g0 (older than the dropped g1) must not survive: %+v", out)
	}
	if containsContent(out, g1) {
		t.Errorf("g1 should have been dropped for exceeding budget: %+v", out)
	}
	if !containsContent(out, g2) {
		t.Errorf("g2 (newest) must be kept: %+v", out)
	}
	if !containsContent(out, historyTrimmedNotice) {
		t.Errorf("expected trim notice in output: %+v", out)
	}
}

func TestCompactMessages_ResultNeverExceedsMaxChars(t *testing.T) {
	var msgs []orclient.Message
	for i := 0; i < 200; i++ {
		msgs = append(msgs,
			orclient.Message{Role: "user", Content: strings.Repeat("q", 40)},
			orclient.Message{Role: "assistant", Content: strings.Repeat("a", 40)},
		)
	}

	maxChars := 500
	out, compacted := compactMessages(msgs, maxChars)
	if !compacted {
		t.Fatalf("expected compaction to occur")
	}
	if got := totalChars(out); got > maxChars {
		t.Errorf("totalChars(out) = %d, want <= %d", got, maxChars)
	}
}

func TestCompactMessages_ShrinksOversizedNewestToolResult(t *testing.T) {
	oldUser := "old-turn"
	newestUser := "newest turn"
	bigToolResult := strings.Repeat("z", truncatedToolResultCap*3)

	msgs := []orclient.Message{
		{Role: "user", Content: oldUser},
		{Role: "user", Content: newestUser},
		{Role: "tool", ToolCallID: "call1", Content: bigToolResult},
	}

	maxChars := len(historyTrimmedNotice) + len(newestUser) + truncatedToolResultCap + len(truncatedToolResultSuffix)

	out, compacted := compactMessages(msgs, maxChars)
	if !compacted {
		t.Fatalf("expected compaction to occur")
	}
	if got := totalChars(out); got > maxChars {
		t.Errorf("totalChars(out) = %d, want <= %d", got, maxChars)
	}
	if !containsContent(out, truncatedToolResultSuffix) {
		t.Errorf("expected the oversized newest tool result to be truncated: %+v", out)
	}
}
