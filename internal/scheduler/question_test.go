package scheduler

import (
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
