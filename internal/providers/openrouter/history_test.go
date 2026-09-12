package openrouter

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestCompactMessages_PreservesBoundedExtractiveDigest(t *testing.T) {
	msgs := []orclient.Message{
		{Role: "user", Content: "Requirement: imports. " + strings.Repeat("old context ", 100)},
		{Role: "assistant", Content: "Outcome: adapter ready. " + strings.Repeat("old context ", 100)},
		{Role: "user", Content: "Second requirement: migration guard. " + strings.Repeat("old context ", 100)},
		{Role: "assistant", Content: "Outcome: done. " + strings.Repeat("old context ", 100)},
		{Role: "user", Content: "Current request: show the final result."},
	}
	maxChars := 1200
	out, compacted := compactMessages(msgs, maxChars)
	if !compacted {
		t.Fatalf("expected compaction to occur")
	}
	if got := totalChars(out); got > maxChars {
		t.Fatalf("totalChars(out) = %d, want <= %d", got, maxChars)
	}
	var digest string
	for _, m := range out {
		if m.Role == "system" {
			if s, ok := m.Content.(string); ok && strings.HasPrefix(s, historyDigestNotice+"\n") {
				digest = s
			}
		}
	}
	if digest == "" {
		t.Fatalf("expected an extractive digest, got %+v", out)
	}
	if len(digest) > historyDigestMaxBytes {
		t.Errorf("digest length = %d, want <= %d", len(digest), historyDigestMaxBytes)
	}
	if !strings.Contains(digest, "Requirement: imports.") || !strings.Contains(digest, "Outcome: done.") {
		t.Errorf("digest must retain earlier requirement and assistant outcome: %q", digest)
	}
	if !strings.Contains(digest, "not verified facts") {
		t.Errorf("digest must identify excerpts as unverified context: %q", digest)
	}
	if !containsContent(out, "Current request: show the final result.") {
		t.Errorf("newest user turn was not retained: %+v", out)
	}
}

func TestQuoteUTF8WithinBudget_BoundsEscapedExcerpt(t *testing.T) {
	quoted := quoteUTF8WithinBudget(strings.Repeat(`\\"`, 200), 32)
	if len(quoted) > 32 {
		t.Fatalf("quoted excerpt length = %d, want <= 32: %q", len(quoted), quoted)
	}
	if !utf8.ValidString(quoted) {
		t.Fatalf("quoted excerpt is not valid UTF-8: %q", quoted)
	}
}

func TestCompactMessages_UTF8SafeAndDoesNotMutateInputWhenCurrentTurnIsOversized(t *testing.T) {
	current := strings.Repeat("текущая инструкция сохраняется ", 80)
	msgs := []orclient.Message{
		{Role: "user", Content: "old requirement"},
		{Role: "assistant", Content: "old outcome"},
		{Role: "user", Content: current},
	}
	original, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	out, compacted := compactMessages(msgs, 700)
	if !compacted {
		t.Fatalf("expected compaction to occur")
	}
	if !utf8.ValidString(current) {
		t.Fatal("test input is not valid UTF-8")
	}
	if !containsContent(out, current) {
		t.Fatalf("current user instruction must remain intact when it exceeds the soft limit")
	}
	for _, m := range out {
		if s, ok := m.Content.(string); ok && !utf8.ValidString(s) {
			t.Errorf("output contains invalid UTF-8: %q", s)
		}
	}
	after, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("compactMessages mutated its input: before=%s after=%s", original, after)
	}
}

func TestCompactMessages_PreservesToolCallPairsInNewestTurn(t *testing.T) {
	largeResult := strings.Repeat("результат ", 200)
	msgs := []orclient.Message{
		{Role: "user", Content: "old requirement"},
		{Role: "assistant", Content: "", ToolCalls: []orclient.ToolCall{
			{ID: "old-call", Type: "function", Function: orclient.FunctionCall{Name: "old_tool", Arguments: "{}"}},
		}},
		{Role: "tool", ToolCallID: "old-call", Content: "old secret result"},
		{Role: "user", Content: "new request"},
		{Role: "assistant", ToolCalls: []orclient.ToolCall{
			{ID: "call-a", Type: "function", Function: orclient.FunctionCall{Name: "first", Arguments: "{}"}},
			{ID: "call-b", Type: "function", Function: orclient.FunctionCall{Name: "second", Arguments: "{}"}},
		}},
		{Role: "tool", ToolCallID: "call-a", Content: largeResult},
		{Role: "tool", ToolCallID: "call-b", Content: largeResult},
		{Role: "assistant", Content: "new outcome"},
	}
	maxChars := len(historyTrimmedNotice) + len("new request") + 5000
	out, compacted := compactMessages(msgs, maxChars)
	if !compacted {
		t.Fatalf("expected compaction to occur")
	}
	if got := totalChars(out); got > maxChars {
		t.Fatalf("totalChars(out) = %d, want <= %d", got, maxChars)
	}
	if containsContent(out, "old secret result") {
		t.Errorf("dropped tool result leaked into compacted history: %+v", out)
	}
	var assistantCalls []string
	var toolIDs []string
	for _, m := range out {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				assistantCalls = append(assistantCalls, tc.ID)
			}
		}
		if m.Role == "tool" {
			toolIDs = append(toolIDs, m.ToolCallID)
		}
	}
	if strings.Join(assistantCalls, ",") != "call-a,call-b" || strings.Join(toolIDs, ",") != "call-a,call-b" {
		t.Errorf("tool-call pairs were not preserved: assistant=%v tools=%v messages=%+v", assistantCalls, toolIDs, out)
	}
	if got := sanitizeHistory(out); len(got) != len(out) {
		t.Errorf("sanitizing compacted history changed valid tool protocol: before=%+v after=%+v", out, got)
	}
}

func BenchmarkCompactMessages_100Turns(b *testing.B) {
	msgs := make([]orclient.Message, 0, 200)
	for i := 0; i < 100; i++ {
		msgs = append(msgs,
			orclient.Message{Role: "user", Content: "requirement " + strings.Repeat("u", 100)},
			orclient.Message{Role: "assistant", Content: "outcome " + strings.Repeat("a", 100)},
		)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compactMessages(msgs, 10000)
	}
}

func TestCompactionOversizedInstructionWithToolTerminates(t *testing.T) {
	current := strings.Repeat("keep current instruction ", 100)
	msgs := []orclient.Message{
		{Role: "user", Content: current},
		{Role: "assistant", ToolCalls: []orclient.ToolCall{{ID: "call", Type: "function", Function: orclient.FunctionCall{Name: "read", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "call", Content: strings.Repeat("result", 500)},
	}
	out, _ := compactMessages(msgs, 700)
	if !containsContent(out, current) {
		t.Fatal("current instruction was lost")
	}
	if len(sanitizeHistory(out)) != len(out) {
		t.Fatal("tool pair was broken")
	}
}

func TestDigestEscapedTextFitsBudget(t *testing.T) {
	groups := [][]orclient.Message{{{Role: "user", Content: strings.Repeat("\\\"", 1000)}}}
	for _, budget := range []int{140, 320, 2048} {
		digest := buildHistoryDigest(groups, budget)
		if len(digest) > budget || !utf8.ValidString(digest) {
			t.Fatalf("budget=%d bytes=%d", budget, len(digest))
		}
	}
}

func TestHistoryDigestLongHistoryKeepsReadableChronologicalExcerpts(t *testing.T) {
	for _, turns := range []int{5, 30, 100, 1000} {
		t.Run(strconv.Itoa(turns), func(t *testing.T) {
			var groups [][]orclient.Message
			for i := 1; i <= turns; i++ {
				groups = append(groups, []orclient.Message{
					{Role: "user", Content: fmt.Sprintf("Requirement %d: preserve these concrete details. %s", i, strings.Repeat("context ", 100))},
					{Role: "assistant", Content: fmt.Sprintf("Outcome %d: implementation and checks complete. %s", i, strings.Repeat("result ", 100))},
				})
			}
			digest := buildHistoryDigest(groups, historyDigestMaxBytes)
			if len(digest) > historyDigestMaxBytes || !strings.Contains(digest, "Requirement 1:") || !strings.Contains(digest, fmt.Sprintf("Outcome %d:", turns)) {
				t.Fatalf("digest lost anchor/latest outcome or exceeded budget: %q", digest)
			}
			previous := 0
			lines := strings.Split(digest, "\n")[1:]
			if len(lines) > 8 {
				t.Fatalf("too many excerpts: %d", len(lines))
			}
			for _, line := range lines {
				var turn int
				if _, err := fmt.Sscanf(line, "- Earlier turn %d", &turn); err != nil || turn < previous {
					t.Fatalf("non-chronological excerpt after %d: %q", previous, line)
				}
				previous = turn
				quoted := line[strings.Index(line, ": ")+2:]
				text, err := strconv.Unquote(quoted)
				if err != nil || len(text) < 32 {
					t.Fatalf("unreadable excerpt %q: %v", quoted, err)
				}
			}
		})
	}
}

func TestTruncateUTF8RepairsInvalidBytesWithinBudget(t *testing.T) {
	for _, tc := range []struct {
		input string
		limit int
		want  string
	}{
		{"abc\xffdef", 100, "abc�def"},
		{"abc\xffdef", 8, "abc�de"},
		{"abc\xffdef", 5, "abc"},
		{"ёжик", 3, "ё"},
		{"ёжик", 0, ""},
	} {
		got := truncateUTF8(tc.input, tc.limit)
		if got != tc.want || !utf8.ValidString(got) || len(got) > tc.limit {
			t.Errorf("truncateUTF8(%q, %d)=%q, want %q", tc.input, tc.limit, got, tc.want)
		}
	}
}

func TestOversizedUnchangedHistoryDoesNotClaimItWasTrimmed(t *testing.T) {
	for _, role := range []string{"system", "user"} {
		msgs := []orclient.Message{{Role: role, Content: strings.Repeat("preserve everything ", 100)}}
		out, compacted := compactMessages(msgs, 100)
		if compacted || !reflect.DeepEqual(out, msgs) {
			t.Fatalf("unchanged %s history reported as trimmed: compacted=%v output=%+v", role, compacted, out)
		}
	}
}
