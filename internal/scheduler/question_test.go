package scheduler

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestDetectQuestionValidBlock covers the documented example format exactly:
// a fenced studioforge-question block with a question and two options.
func TestDetectQuestionValidBlock(t *testing.T) {
	text := "Here is what I found.\n\n" +
		"```studioforge-question\n" +
		`{"question": "Which mesh format should I use?", "options": [{"label": "FBX", "description": "Standard interchange format"}, {"label": "OBJ", "description": "Simpler, wider tool support"}]}` +
		"\n```\n"
	block, ok := detectQuestion(text)
	if !ok {
		t.Fatalf("expected a question to be detected in %q", text)
	}
	if block.Question != "Which mesh format should I use?" {
		t.Errorf("question=%q", block.Question)
	}
	if len(block.Options) != 2 {
		t.Fatalf("options=%+v, want 2", block.Options)
	}
	if block.Options[0].Label != "FBX" || block.Options[0].Description != "Standard interchange format" {
		t.Errorf("options[0]=%+v", block.Options[0])
	}
	if block.Options[1].Label != "OBJ" || block.Options[1].Description != "Simpler, wider tool support" {
		t.Errorf("options[1]=%+v", block.Options[1])
	}
}

// TestDetectQuestionMalformedJSON must never treat a broken fence as a
// question: no crash, and no false positive that would wrongly stop the run.
func TestDetectQuestionMalformedJSON(t *testing.T) {
	cases := []string{
		"```studioforge-question\n{not valid json\n```",
		"```studioforge-question\n{\"question\": \"missing options\"}\n```",
		"```studioforge-question\n{\"options\": [{\"label\": \"A\", \"description\": \"\"}]}\n```",
		"```studioforge-question\n{\"question\": \"\", \"options\": [{\"label\": \"A\", \"description\": \"\"}]}\n```",
		"```studioforge-question\n{\"question\": \"Q\", \"options\": []}\n```",
	}
	for _, text := range cases {
		if _, ok := detectQuestion(text); ok {
			t.Errorf("detectQuestion(%q) matched, want no match", text)
		}
	}
}

// TestDetectQuestionPartialBlock is the mid-stream case: the opening fence
// and part of the JSON have arrived but the closing fence has not, exactly
// what a partial/streaming delta chunk looks like before the message is
// fully buffered. It must not be falsely matched.
func TestDetectQuestionPartialBlock(t *testing.T) {
	cases := []string{
		"```studioforge-question\n",
		"```studioforge-question\n{\"question\": \"Which mesh",
		"```studioforge-question\n{\"question\": \"Which mesh format should I use?\", \"options\": [{\"label\": \"FBX\"",
		"Some prose without any fence at all.",
	}
	for _, text := range cases {
		if _, ok := detectQuestion(text); ok {
			t.Errorf("detectQuestion(%q) matched a partial/incomplete block, want no match", text)
		}
	}
}

// TestMessageTextShapes covers the three provider payload shapes a fully
// buffered assistant message can arrive in: the mock provider's flat "text"
// field, Codex's item.text, and Claude's message.content[] entries.
func TestMessageTextShapes(t *testing.T) {
	cases := []struct {
		name    string
		payload any
		want    string
	}{
		{"mock flat text", map[string]any{"text": "hello"}, "hello"},
		{"codex item text", map[string]any{"item": map[string]any{"type": "agent_message", "text": "done"}}, "done"},
		{"claude content array", map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "hi"}}}}, "hi"},
		{"not a map", "just a string", ""},
		{"empty map", map[string]any{}, ""},
	}
	for _, c := range cases {
		if got := messageText(c.payload); got != c.want {
			t.Errorf("%s: messageText=%q want %q", c.name, got, c.want)
		}
	}
}

// TestIsFullyBufferedMessage checks the RawType markers each provider uses
// to distinguish a complete message from a streaming delta chunk.
func TestIsFullyBufferedMessage(t *testing.T) {
	cases := []struct {
		rawType string
		want    bool
	}{
		{"assistant", true},          // Claude: a complete non-streamed message
		{"stream_event", false},      // Claude: a partial delta chunk
		{"assistant.final", true},    // mock provider: the buffered final message
		{"assistant.partial", false}, // mock provider: a streaming step
		{"item.completed", true},     // Codex: the buffered final item
		{"item.started", false},      // Codex: item construction begun
		{"item.updated", false},      // Codex: item construction in progress
	}
	for _, c := range cases {
		if got := isFullyBufferedMessage(c.rawType); got != c.want {
			t.Errorf("isFullyBufferedMessage(%q) = %v, want %v", c.rawType, got, c.want)
		}
	}
}

// TestQuestionEndToEnd drives the mock provider's "question test" scenario
// through the real scheduler run loop and checks that a "question" event is
// published and the run lands on waiting_decision instead of completed.
func TestQuestionEndToEnd(t *testing.T) {
	manager, provider, store, ctx := newHarness(t)
	_ = provider
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1, Prompt: "please run a question test"})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "waiting_decision", 3*time.Second)
	events, err := store.EventsAfter(ctx, 0, "demo-obby", run.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Type != "question" {
			continue
		}
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			t.Fatalf("question payload is not a map: %+v", event.Payload)
		}
		if payload["question"] == "" || payload["question"] == nil {
			t.Fatalf("question payload missing question: %+v", payload)
		}
		if payload["options"] == nil {
			t.Fatalf("question payload missing options: %+v", payload)
		}
		found = true
	}
	if !found {
		t.Fatal("no question event was published for the question-test scenario")
	}
}

// fence wraps a JSON body in a studioforge-question fenced block exactly as
// a coding agent would emit it: the info string alone on its own line, the
// JSON body, then the closing fence alone on its own line.
func fence(body string) string {
	return "```studioforge-question\n" + body + "\n```"
}

// TestDetectQuestionRejectsSingleOption enforces the v2 minimum of two
// options: a block with only one option must not be treated as a question.
func TestDetectQuestionRejectsSingleOption(t *testing.T) {
	text := fence(`{"question": "Pick one", "options": [{"label": "A", "description": "aa"}]}`)
	if _, ok := detectQuestion(text); ok {
		t.Errorf("detectQuestion(%q) matched a single-option block, want no match", text)
	}
}

// TestDetectQuestionRejectsFiveOptions enforces the v2 maximum of four
// options: a block with five options must not be treated as a question.
func TestDetectQuestionRejectsFiveOptions(t *testing.T) {
	text := fence(`{"question": "Pick one", "options": [{"label": "A"}, {"label": "B"}, {"label": "C"}, {"label": "D"}, {"label": "E"}]}`)
	if _, ok := detectQuestion(text); ok {
		t.Errorf("detectQuestion(%q) matched a five-option block, want no match", text)
	}
}

// TestDetectQuestionRejectsBlankLabel rejects an option whose label is empty
// once whitespace is trimmed, even though the raw JSON field is non-empty.
func TestDetectQuestionRejectsBlankLabel(t *testing.T) {
	text := fence(`{"question": "Pick one", "options": [{"label": "   ", "description": "aa"}, {"label": "B", "description": "bb"}]}`)
	if _, ok := detectQuestion(text); ok {
		t.Errorf("detectQuestion(%q) matched a blank-label option, want no match", text)
	}
}

// TestDetectQuestionRejectsDuplicateLabels rejects two options whose labels
// are identical once trimmed, even though the raw JSON differs in
// surrounding whitespace.
func TestDetectQuestionRejectsDuplicateLabels(t *testing.T) {
	text := fence(`{"question": "Pick one", "options": [{"label": "A", "description": "aa"}, {"label": " A ", "description": "bb"}]}`)
	if _, ok := detectQuestion(text); ok {
		t.Errorf("detectQuestion(%q) matched duplicate labels, want no match", text)
	}
}

// TestDetectQuestionRejectsQuestionTooLong rejects a question whose trimmed
// rune length exceeds the 2000-rune cap.
func TestDetectQuestionRejectsQuestionTooLong(t *testing.T) {
	longQuestion := strings.Repeat("q", maxQuestionLength+1)
	body := fmt.Sprintf(`{"question": %q, "options": [{"label": "A", "description": "aa"}, {"label": "B", "description": "bb"}]}`, longQuestion)
	if _, ok := detectQuestion(fence(body)); ok {
		t.Errorf("detectQuestion matched a question over %d runes, want no match", maxQuestionLength)
	}
}

// TestDetectQuestionRejectsLabelTooLong rejects an option label whose
// trimmed rune length exceeds the 120-rune cap.
func TestDetectQuestionRejectsLabelTooLong(t *testing.T) {
	longLabel := strings.Repeat("a", maxOptionLabelLength+1)
	body := fmt.Sprintf(`{"question": "Pick one", "options": [{"label": %q, "description": "aa"}, {"label": "B", "description": "bb"}]}`, longLabel)
	if _, ok := detectQuestion(fence(body)); ok {
		t.Errorf("detectQuestion matched a label over %d runes, want no match", maxOptionLabelLength)
	}
}

// TestDetectQuestionRejectsDescriptionTooLong rejects an option description
// whose rune length exceeds the 600-rune cap.
func TestDetectQuestionRejectsDescriptionTooLong(t *testing.T) {
	longDesc := strings.Repeat("d", maxOptionDescLength+1)
	body := fmt.Sprintf(`{"question": "Pick one", "options": [{"label": "A", "description": %q}, {"label": "B", "description": "bb"}]}`, longDesc)
	if _, ok := detectQuestion(fence(body)); ok {
		t.Errorf("detectQuestion matched a description over %d runes, want no match", maxOptionDescLength)
	}
}

// TestDetectQuestionRejectsInvalidJSONSyntax rejects a fence whose body is
// not valid JSON at all (here, trailing commas), independent of the
// missing-field cases already covered by TestDetectQuestionMalformedJSON.
func TestDetectQuestionRejectsInvalidJSONSyntax(t *testing.T) {
	text := fence(`{"question": "Pick one", "options": [{"label": "A",},]}`)
	if _, ok := detectQuestion(text); ok {
		t.Errorf("detectQuestion(%q) matched invalid JSON, want no match", text)
	}
}

// TestDetectQuestionRejectsTwoFencedBlocks requires exactly one
// studioforge-question fence per message: two valid blocks in the same
// message must not be treated as a question, since it is ambiguous which one
// the caller should act on.
func TestDetectQuestionRejectsTwoFencedBlocks(t *testing.T) {
	block := `{"question": "Pick one", "options": [{"label": "A", "description": "aa"}, {"label": "B", "description": "bb"}]}`
	text := fence(block) + "\n\nActually, here is another one:\n\n" + fence(block)
	if _, ok := detectQuestion(text); ok {
		t.Errorf("detectQuestion(%q) matched a message with two fenced blocks, want no match", text)
	}
}

// TestDetectQuestionRejectsOversizedBody rejects a fence whose JSON body
// exceeds the 8192-byte cap, before it is even parsed as JSON.
func TestDetectQuestionRejectsOversizedBody(t *testing.T) {
	hugeDesc := strings.Repeat("x", maxQuestionBodyBytes)
	body := fmt.Sprintf(`{"question": "Pick one", "options": [{"label": "A", "description": %q}, {"label": "B", "description": "bb"}]}`, hugeDesc)
	if len(body) <= maxQuestionBodyBytes {
		t.Fatalf("test body is %d bytes, want more than %d to exercise the cap", len(body), maxQuestionBodyBytes)
	}
	if _, ok := detectQuestion(fence(body)); ok {
		t.Errorf("detectQuestion matched a body over %d bytes, want no match", maxQuestionBodyBytes)
	}
}

// TestDetectQuestionRejectsNoFence confirms plain prose with no fence marker
// at all is never mistaken for a question.
func TestDetectQuestionRejectsNoFence(t *testing.T) {
	text := "Just a plain message with no fence markers at all."
	if _, ok := detectQuestion(text); ok {
		t.Errorf("detectQuestion(%q) matched text with no fence, want no match", text)
	}
}

// TestDetectQuestionAcceptsTwoOptions is the minimum valid shape: exactly
// two options, and the parsed block must carry through the question text
// and both options.
func TestDetectQuestionAcceptsTwoOptions(t *testing.T) {
	text := fence(`{"question": "Pick one", "options": [{"label": "A", "description": "aa"}, {"label": "B", "description": "bb"}]}`)
	block, ok := detectQuestion(text)
	if !ok {
		t.Fatalf("expected a question to be detected in %q", text)
	}
	if block.Question != "Pick one" {
		t.Errorf("question=%q", block.Question)
	}
	if len(block.Options) != 2 {
		t.Fatalf("options=%+v, want 2", block.Options)
	}
}

// TestDetectQuestionAcceptsFourOptions is the maximum valid shape: exactly
// four options, and the parsed block must carry through the question text
// and all four options.
func TestDetectQuestionAcceptsFourOptions(t *testing.T) {
	text := fence(`{"question": "Pick one", "options": [{"label": "A", "description": "aa"}, {"label": "B", "description": "bb"}, {"label": "C", "description": "cc"}, {"label": "D", "description": "dd"}]}`)
	block, ok := detectQuestion(text)
	if !ok {
		t.Fatalf("expected a question to be detected in %q", text)
	}
	if block.Question != "Pick one" {
		t.Errorf("question=%q", block.Question)
	}
	if len(block.Options) != 4 {
		t.Fatalf("options=%+v, want 4", block.Options)
	}
}

// TestDetectQuestionAcceptsEmbeddedInProse checks a valid block surrounded
// by ordinary prose text is still detected: the fence does not need to be
// the entire message.
func TestDetectQuestionAcceptsEmbeddedInProse(t *testing.T) {
	text := "Here is what I found, let me know which you prefer.\n\n" +
		fence(`{"question": "Which mesh format should I use?", "options": [{"label": "FBX", "description": "Standard interchange format"}, {"label": "OBJ", "description": "Simpler, wider tool support"}]}`) +
		"\n\nI can proceed once you pick one.\n"
	block, ok := detectQuestion(text)
	if !ok {
		t.Fatalf("expected a question to be detected in %q", text)
	}
	if block.Question != "Which mesh format should I use?" {
		t.Errorf("question=%q", block.Question)
	}
	if len(block.Options) != 2 {
		t.Fatalf("options=%+v, want 2", block.Options)
	}
}
