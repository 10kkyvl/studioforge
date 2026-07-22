package openrouter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/10kkyvl/studioforge/internal/providers"
)

type fakeConversationStore struct {
	mu   sync.Mutex
	byID map[string][]StoredMessage
}

func newFakeConversationStore() *fakeConversationStore {
	return &fakeConversationStore{byID: map[string][]StoredMessage{}}
}

func (f *fakeConversationStore) Load(_ context.Context, threadID string) ([]StoredMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]StoredMessage, len(f.byID[threadID]))
	copy(out, f.byID[threadID])
	return out, nil
}

func (f *fakeConversationStore) Append(_ context.Context, threadID, _ string, msg StoredMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[threadID] = append(f.byID[threadID], msg)
	return nil
}

func (f *fakeConversationStore) seed(threadID string, msgs ...StoredMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[threadID] = append(f.byID[threadID], msgs...)
}

func messagesContain(msgs []any, role, substr string) bool {
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if role != "" && mm["role"] != role {
			continue
		}
		if strings.Contains(fmt.Sprint(mm["content"]), substr) {
			return true
		}
	}
	return false
}

func TestConversation_NextTurnResumesHistory(t *testing.T) {
	store := newFakeConversationStore()
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: fmt.Sprintf("answer-%d", call)}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	dir := t.TempDir()

	_, result := runProvider(t, provider, providers.RunRequest{RunID: "run1", ProjectID: "p1", ThreadID: "thread1", WorkingDirectory: dir, Prompt: "A", Model: "test-model"})
	if result.Err != nil {
		t.Fatalf("run1 result = %+v", result)
	}

	_, result = runProvider(t, provider, providers.RunRequest{RunID: "run2", ProjectID: "p1", ThreadID: "thread1", WorkingDirectory: dir, Prompt: "B", Model: "test-model"})
	if result.Err != nil {
		t.Fatalf("run2 result = %+v", result)
	}

	if log.count() != 2 {
		t.Fatalf("want 2 HTTP calls, got %d", log.count())
	}
	body := log.body(1)
	msgs, _ := body["messages"].([]any)
	if !messagesContain(msgs, "user", "A") {
		t.Errorf("run2 request did not carry run1's user message: %+v", msgs)
	}
	if !messagesContain(msgs, "assistant", "answer-0") {
		t.Errorf("run2 request did not carry run1's assistant answer: %+v", msgs)
	}
	if !messagesContain(msgs, "user", "B") {
		t.Errorf("run2 request did not carry its own new prompt: %+v", msgs)
	}
	userIdx, aIdx, bIdx := -1, -1, -1
	for i, m := range msgs {
		mm, _ := m.(map[string]any)
		content := fmt.Sprint(mm["content"])
		if mm["role"] == "user" && strings.Contains(content, "A") && userIdx == -1 {
			userIdx = i
		}
		if mm["role"] == "assistant" && strings.Contains(content, "answer-0") {
			aIdx = i
		}
		if mm["role"] == "user" && strings.Contains(content, "B") {
			bIdx = i
		}
	}
	if !(userIdx < aIdx && aIdx < bIdx) {
		t.Errorf("history out of order: userIdx=%d aIdx=%d bIdx=%d, msgs=%+v", userIdx, aIdx, bIdx, msgs)
	}
}

func TestConversation_RestartPersistenceReplaysSeededHistory(t *testing.T) {
	store := newFakeConversationStore()
	store.seed("thread-restart",
		StoredMessage{Role: "user", Content: "seeded question"},
		StoredMessage{Role: "assistant", Content: "seeded answer"},
	)
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "fresh answer"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	dir := t.TempDir()

	_, result := runProvider(t, provider, providers.RunRequest{RunID: "run1", ProjectID: "p1", ThreadID: "thread-restart", WorkingDirectory: dir, Prompt: "new question", Model: "test-model"})
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}

	body := log.body(0)
	msgs, _ := body["messages"].([]any)
	if !messagesContain(msgs, "user", "seeded question") {
		t.Errorf("first request did not carry seeded user message: %+v", msgs)
	}
	if !messagesContain(msgs, "assistant", "seeded answer") {
		t.Errorf("first request did not carry seeded assistant message: %+v", msgs)
	}
}

func TestConversation_InteractiveQuestionPersistsAndContinues(t *testing.T) {
	store := newFakeConversationStore()
	const question = "Which color scheme?\n```studioforge-question\n{\"options\":[\"red\",\"blue\"]}\n```"
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: question}, FinishReason: "stop"}}}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "Using red."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	dir := t.TempDir()

	_, result := runProvider(t, provider, providers.RunRequest{RunID: "run1", ProjectID: "p1", ThreadID: "thread-q", WorkingDirectory: dir, Prompt: "pick a scheme", Model: "test-model"})
	if result.Err != nil {
		t.Fatalf("run1 result = %+v", result)
	}
	_, result = runProvider(t, provider, providers.RunRequest{RunID: "run2", ProjectID: "p1", ThreadID: "thread-q", WorkingDirectory: dir, Prompt: "red", Model: "test-model"})
	if result.Err != nil {
		t.Fatalf("run2 result = %+v", result)
	}

	body := log.body(1)
	msgs, _ := body["messages"].([]any)
	if !messagesContain(msgs, "assistant", "studioforge-question") {
		t.Errorf("run2 request did not carry run1's interactive question: %+v", msgs)
	}
}

func TestConversation_SecondRunOnSameThreadLoadsFirstRunsMessages(t *testing.T) {
	store := newFakeConversationStore()
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "ok"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	dir := t.TempDir()

	_, result := runProvider(t, provider, providers.RunRequest{RunID: "run1", ProjectID: "p1", ThreadID: "thread-c", WorkingDirectory: dir, Prompt: "original prompt", Model: "test-model"})
	if result.Err != nil {
		t.Fatalf("run1 result = %+v", result)
	}
	_, result = runProvider(t, provider, providers.RunRequest{RunID: "run2", ProjectID: "p1", ThreadID: "thread-c", WorkingDirectory: dir, Prompt: "the build broke, fix it", Model: "test-model"})
	if result.Err != nil {
		t.Fatalf("run2 result = %+v", result)
	}

	body := log.body(1)
	msgs, _ := body["messages"].([]any)
	if !messagesContain(msgs, "user", "original prompt") {
		t.Errorf("correction-style run2 did not load run1's messages: %+v", msgs)
	}
}

func TestConversation_CompactionTrimsHistoryAndEmitsStatus(t *testing.T) {
	store := newFakeConversationStore()
	SetMaxHistoryChars(2000)
	t.Cleanup(func() { SetMaxHistoryChars(0) })

	for i := 0; i < 30; i++ {
		store.seed("thread-big",
			StoredMessage{Role: "user", Content: fmt.Sprintf("question number %d, %s", i, strings.Repeat("x", 200))},
			StoredMessage{Role: "assistant", Content: fmt.Sprintf("answer number %d, %s", i, strings.Repeat("y", 200))},
		)
	}
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "final answer"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	dir := t.TempDir()

	events, result := runProvider(t, provider, providers.RunRequest{RunID: "run1", ProjectID: "p1", ThreadID: "thread-big", WorkingDirectory: dir, Prompt: "the newest question", SystemPrompt: "you are an agent", Model: "test-model"})
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}

	compactedEvents := findEvents(events, "status", "openrouter.compacted")
	if len(compactedEvents) != 1 {
		t.Fatalf("want exactly 1 openrouter.compacted status event, got %d", len(compactedEvents))
	}

	body := log.body(0)
	msgs, _ := body["messages"].([]any)
	rawBody := log.bodies[0]
	seeded := 0
	for i := 0; i < 30; i++ {
		if strings.Contains(string(rawBody), fmt.Sprintf("question number %d", i)) {
			seeded++
		}
	}
	if seeded == 30 {
		t.Fatalf("expected some early history to be dropped, but all 30 seeded turns are present")
	}
	if !messagesContain(msgs, "system", "you are an agent") {
		t.Errorf("compaction must preserve the system prompt: %+v", msgs)
	}
	if !messagesContain(msgs, "system", "[Earlier conversation history was trimmed to fit the model's context window.]") {
		t.Errorf("compaction must insert the exact fixed trim note: %+v", msgs)
	}
	if !messagesContain(msgs, "user", "the newest question") {
		t.Errorf("compaction must keep the newest user turn: %+v", msgs)
	}
}

func TestConversation_ModelChangeBetweenRunsStillReplaysHistory(t *testing.T) {
	store := newFakeConversationStore()
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "ok"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	dir := t.TempDir()

	_, result := runProvider(t, provider, providers.RunRequest{RunID: "run1", ProjectID: "p1", ThreadID: "thread-model", WorkingDirectory: dir, Prompt: "first prompt", Model: "model-x"})
	if result.Err != nil {
		t.Fatalf("run1 result = %+v", result)
	}
	_, result = runProvider(t, provider, providers.RunRequest{RunID: "run2", ProjectID: "p1", ThreadID: "thread-model", WorkingDirectory: dir, Prompt: "second prompt", Model: "model-y"})
	if result.Err != nil {
		t.Fatalf("run2 result = %+v", result)
	}

	body := log.body(1)
	if body["model"] != "model-y" {
		t.Errorf("run2 request model = %v, want model-y", body["model"])
	}
	msgs, _ := body["messages"].([]any)
	if !messagesContain(msgs, "user", "first prompt") {
		t.Errorf("run2 (different model) did not carry run1's history: %+v", msgs)
	}
}

func TestConversation_AttachmentsRoundTripAndDanglingToolCallSanitized(t *testing.T) {
	store := newFakeConversationStore()
	store.seed("thread-attach",
		StoredMessage{Role: "user", Content: "here are some files", Attachments: []string{"screenshot.png", "log.txt"}},
		StoredMessage{Role: "assistant", ToolCallsJSON: `[{"id":"call_dangling","type":"function","function":{"name":"list_dir","arguments":"{}"}}]`},
	)
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "continuing"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	dir := t.TempDir()

	_, result := runProvider(t, provider, providers.RunRequest{RunID: "run1", ProjectID: "p1", ThreadID: "thread-attach", WorkingDirectory: dir, Prompt: "continue", Model: "test-model"})
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}

	body := log.body(0)
	msgs, _ := body["messages"].([]any)

	dangleIdx, toolIdx := -1, -1
	for i, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "assistant" {
			if calls, ok := mm["tool_calls"].([]any); ok {
				for _, c := range calls {
					cm, _ := c.(map[string]any)
					if cm["id"] == "call_dangling" {
						dangleIdx = i
					}
				}
			}
		}
		if mm["role"] == "tool" && mm["tool_call_id"] == "call_dangling" {
			toolIdx = i
			if !strings.Contains(fmt.Sprint(mm["content"]), "unavailable") {
				t.Errorf("synthesized tool result should note unavailability: %+v", mm)
			}
		}
	}
	if dangleIdx == -1 {
		t.Fatalf("dangling assistant tool_call not found in request: %+v", msgs)
	}
	if toolIdx != dangleIdx+1 {
		t.Errorf("synthesized tool result must immediately follow the assistant message: dangleIdx=%d toolIdx=%d", dangleIdx, toolIdx)
	}

	store.mu.Lock()
	persisted := append([]StoredMessage{}, store.byID["thread-attach"]...)
	store.mu.Unlock()
	found := false
	for _, m := range persisted {
		if m.Role == "user" && len(m.Attachments) == 2 && m.Attachments[0] == "screenshot.png" {
			found = true
		}
	}
	if !found {
		t.Errorf("attachment list did not round-trip through the store: %+v", persisted)
	}
}

func TestConversation_ResumeRebuildsStoredImagesForVisionModel(t *testing.T) {
	dir := t.TempDir()
	rel := ".studioforge/attachments/seeded.png"
	writeTestPNG(t, dir, rel)

	store := newFakeConversationStore()
	store.seed("thread-img",
		StoredMessage{Role: "user", Content: "look at this", Attachments: []string{rel}},
		StoredMessage{Role: "assistant", Content: "I see a red square."},
	)
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "ok"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	provider.SetModelInfo(func(id string) (ModelInfo, bool) { return ModelInfo{Vision: true}, true })

	_, result := runProvider(t, provider, providers.RunRequest{RunID: "resume-img-1", ProjectID: "p1", ThreadID: "thread-img", WorkingDirectory: dir, Prompt: "continue", Model: "vision-model"})
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	body := log.body(0)
	msgs, _ := body["messages"].([]any)
	if parts := findImageURLParts(msgs); len(parts) != 1 {
		t.Errorf("expected the rebuilt stored user message to include exactly 1 image_url part, got %d: %+v", len(parts), msgs)
	}
}

func TestConversation_ResumeNonVisionModelUsesStoredTextOnly(t *testing.T) {
	dir := t.TempDir()
	rel := ".studioforge/attachments/seeded.png"
	writeTestPNG(t, dir, rel)

	store := newFakeConversationStore()
	store.seed("thread-img-textonly", StoredMessage{Role: "user", Content: "look at this", Attachments: []string{rel}})
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "ok"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	// No SetModelInfo call: the model is unknown, so vision defaults to false.

	_, result := runProvider(t, provider, providers.RunRequest{RunID: "resume-img-2", ProjectID: "p1", ThreadID: "thread-img-textonly", WorkingDirectory: dir, Prompt: "continue", Model: "text-model"})
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	body := log.body(0)
	msgs, _ := body["messages"].([]any)
	if parts := findImageURLParts(msgs); len(parts) != 0 {
		t.Errorf("non-vision model must not rebuild image parts, got %+v", parts)
	}
	if !messagesContain(msgs, "user", "look at this") {
		t.Errorf("expected the stored text to still be present: %+v", msgs)
	}
}

func TestConversation_ResumeMissingAttachmentSkippedWithoutError(t *testing.T) {
	dir := t.TempDir()
	store := newFakeConversationStore()
	store.seed("thread-img-missing", StoredMessage{Role: "user", Content: "look at this", Attachments: []string{".studioforge/attachments/missing.png"}})
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "ok"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	provider.SetModelInfo(func(id string) (ModelInfo, bool) { return ModelInfo{Vision: true}, true })

	_, result := runProvider(t, provider, providers.RunRequest{RunID: "resume-img-3", ProjectID: "p1", ThreadID: "thread-img-missing", WorkingDirectory: dir, Prompt: "continue", Model: "vision-model"})
	if result.Err != nil {
		t.Fatalf("expected a clean run despite the missing attachment, got %+v", result)
	}
	body := log.body(0)
	msgs, _ := body["messages"].([]any)
	if parts := findImageURLParts(msgs); len(parts) != 0 {
		t.Errorf("expected the missing attachment to be skipped, got %+v", parts)
	}
	if !messagesContain(msgs, "user", "look at this") {
		t.Errorf("expected the stored text to still be present: %+v", msgs)
	}
}
