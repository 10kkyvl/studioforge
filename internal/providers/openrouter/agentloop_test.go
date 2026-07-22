package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/processes"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/orclient"
)

type wireFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type wireToolCallDelta struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function wireFunctionDelta `json:"function,omitempty"`
}

type wireDelta struct {
	Content   string              `json:"content,omitempty"`
	Reasoning string              `json:"reasoning,omitempty"`
	ToolCalls []wireToolCallDelta `json:"tool_calls,omitempty"`
}

func TestAgentLoop_ReasoningIsOnlyANeutralIndicator(t *testing.T) {
	const hidden = "raw-hidden-reasoning"
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{
			{Choices: []wireChoice{{Delta: wireDelta{Reasoning: hidden}}}},
			{Choices: []wireChoice{{Delta: wireDelta{Content: "safe"}, FinishReason: "stop"}}},
		}
	})
	events, result := runProvider(t, newTestProvider(t, srv), providers.RunRequest{RunID: "reasoning", ProjectID: "p1", WorkingDirectory: t.TempDir(), Prompt: "hi", Model: "m"})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	encoded, _ := json.Marshal(events)
	if strings.Contains(string(encoded), hidden) {
		t.Fatalf("raw reasoning escaped into provider events: %s", encoded)
	}
	indicators := findEvents(events, "status", "openrouter.reasoning")
	if len(indicators) != 1 {
		t.Fatalf("reasoning indicators = %d", len(indicators))
	}
}

func TestAgentLoop_RetriesExtendedPreStreamOutage(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call <= 4 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	_, result := runProvider(t, newTestProvider(t, srv), providers.RunRequest{RunID: "extended-retry", ProjectID: "p1", WorkingDirectory: t.TempDir(), Prompt: "hi", Model: "m"})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 5 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestAgentLoop_RejectsKnownModelWithoutTools(t *testing.T) {
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk { return nil })
	provider := newTestProvider(t, srv)
	provider.SetModelInfo(func(string) (ModelInfo, bool) {
		return ModelInfo{CapabilitiesKnown: true, Verified: true, PriceKnown: true}, true
	})
	_, result := runProvider(t, provider, providers.RunRequest{RunID: "no-tools", ProjectID: "p1", WorkingDirectory: t.TempDir(), Prompt: "go", Model: "chat-only", AllowUnverifiedModel: true})
	if result.Err == nil || log.count() != 0 {
		t.Fatalf("result=%+v calls=%d", result, log.count())
	}
}

func TestAgentLoop_RequiresConfirmationForUnknownCapabilities(t *testing.T) {
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk { return nil })
	provider := newTestProvider(t, srv)
	provider.SetModelInfo(func(string) (ModelInfo, bool) {
		return ModelInfo{PriceKnown: true}, true
	})
	_, result := runProvider(t, provider, providers.RunRequest{RunID: "unknown-tools", ProjectID: "p1", WorkingDirectory: t.TempDir(), Prompt: "go", Model: "unknown"})
	if result.Err == nil || log.count() != 0 {
		t.Fatalf("result=%+v calls=%d", result, log.count())
	}
}

func TestAgentLoop_AllowsConfirmedFreeRouterWithinBudget(t *testing.T) {
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "done"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetModelInfo(func(string) (ModelInfo, bool) {
		return ModelInfo{PriceKnown: true}, true
	})
	_, result := runProvider(t, provider, providers.RunRequest{RunID: "free-router", ProjectID: "p1", WorkingDirectory: t.TempDir(), Prompt: "go", Model: "openrouter/free", AllowUnverifiedModel: true, MaxBudget: 0.01})
	if result.Err != nil || log.count() != 1 || result.Cost != 0 {
		t.Fatalf("result=%+v calls=%d", result, log.count())
	}
}

type wireChoice struct {
	Delta        wireDelta `json:"delta"`
	FinishReason string    `json:"finish_reason,omitempty"`
}

type wireUsageDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type wireUsage struct {
	PromptTokens        int               `json:"prompt_tokens"`
	CompletionTokens    int               `json:"completion_tokens"`
	TotalTokens         int               `json:"total_tokens"`
	Cost                float64           `json:"cost,omitempty"`
	PromptTokensDetails *wireUsageDetails `json:"prompt_tokens_details,omitempty"`
}

type wireChunk struct {
	Model   string       `json:"model,omitempty"`
	Choices []wireChoice `json:"choices,omitempty"`
	Usage   *wireUsage   `json:"usage,omitempty"`
}

type requestLog struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (r *requestLog) add(b []byte) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bodies = append(r.bodies, b)
	return len(r.bodies) - 1
}

func (r *requestLog) body(i int) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var v map[string]any
	_ = json.Unmarshal(r.bodies[i], &v)
	return v
}

func (r *requestLog) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func newMockServer(t *testing.T, respond func(call int, body []byte) []wireChunk) (*httptest.Server, *requestLog) {
	t.Helper()
	log := &requestLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		idx := log.add(body)
		chunks := respond(idx, body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			b, _ := json.Marshal(c)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(srv.Close)
	return srv, log
}

func newTestProvider(t *testing.T, srv *httptest.Server) *Provider {
	t.Helper()
	sup := processes.NewSupervisor()
	t.Cleanup(func() { _ = sup.Close(context.Background()) })
	p := New(sup)
	p.SetBaseURL(srv.URL)
	p.SetKeySource(func() string { return "test-key" })
	return p
}

func runProvider(t *testing.T, provider *Provider, req providers.RunRequest) ([]providers.Event, providers.Result) {
	t.Helper()
	h, err := provider.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	var events []providers.Event
	for e := range h.Events() {
		events = append(events, e)
	}
	return events, h.Wait()
}

func findEvents(events []providers.Event, evtType, rawType string) []providers.Event {
	var out []providers.Event
	for _, e := range events {
		if e.Type == evtType && (rawType == "" || e.RawType == rawType) {
			out = append(out, e)
		}
	}
	return out
}

func TestAgentLoop_FinalAnswerNoTools(t *testing.T) {
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{
			Choices: []wireChoice{{Delta: wireDelta{Content: "All done."}, FinishReason: "stop"}},
			Usage:   &wireUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, Cost: 0.01},
		}}
	})
	provider := newTestProvider(t, srv)
	dir := t.TempDir()
	req := providers.RunRequest{RunID: "run1", ProjectID: "p1", WorkingDirectory: dir, Prompt: "say hi", Model: "test-model"}

	events, result := runProvider(t, provider, req)

	msgs := findEvents(events, "message", "openrouter.message")
	if len(msgs) != 1 {
		t.Fatalf("want 1 final message event, got %d: %+v", len(msgs), events)
	}
	payload, _ := msgs[0].Payload.(map[string]any)
	if payload["text"] != "All done." {
		t.Errorf("message text = %v, want %q", payload["text"], "All done.")
	}
	usageEvents := findEvents(events, "usage", "openrouter.usage")
	if len(usageEvents) != 1 {
		t.Fatalf("want 1 usage event, got %d", len(usageEvents))
	}
	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %+v, want ExitCode 0 Err nil", result)
	}
	if result.Usage.InputTokens != 100 || result.Usage.OutputTokens != 20 {
		t.Errorf("result usage = %+v", result.Usage)
	}
	if result.Cost != 0.01 {
		t.Errorf("result cost = %v, want 0.01", result.Cost)
	}
}

func TestProviderRejectsDuplicateActiveRunID(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		b, _ := json.Marshal(wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "done"}, FinishReason: "stop"}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
		flusher.Flush()
	}))
	defer srv.Close()
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "duplicate", ProjectID: "p1", WorkingDirectory: t.TempDir(), Prompt: "hi", Model: "m"}
	handle, err := provider.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := provider.Start(context.Background(), req); err == nil {
		t.Fatal("duplicate active run ID was accepted")
	}
	close(release)
	for range handle.Events() {
	}
	_ = handle.Wait()
}

func TestAgentLoop_SingleToolCall(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{
				Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
					{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "read_file", Arguments: `{"path":"hello.txt"}`}},
				}}, FinishReason: "tool_calls"}},
			}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "The file says hello world."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "run2", ProjectID: "p1", WorkingDirectory: dir, Prompt: "read the file", Model: "test-model"}

	events, result := runProvider(t, provider, req)

	calls := findEvents(events, "tool", "tool.call")
	results := findEvents(events, "tool", "tool.result")
	if len(calls) != 1 || len(results) != 1 {
		t.Fatalf("want 1 tool.call + 1 tool.result, got %d/%d", len(calls), len(results))
	}
	resPayload, _ := results[0].Payload.(map[string]any)
	if resPayload["isError"] == true {
		t.Fatalf("tool result reported error: %+v", resPayload)
	}
	if !strings.Contains(fmt.Sprint(resPayload["result"]), "hello world") {
		t.Errorf("tool result = %v, want to contain file content", resPayload["result"])
	}
	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if log.count() != 2 {
		t.Fatalf("want 2 HTTP calls, got %d", log.count())
	}
	body1 := log.body(1)
	msgs, _ := body1["messages"].([]any)
	found := false
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" && strings.Contains(fmt.Sprint(mm["content"]), "hello world") {
			found = true
		}
	}
	if !found {
		t.Errorf("second request did not carry the tool result message: %+v", msgs)
	}
}

func TestAgentLoop_RetryAfterToolResultDoesNotRepeatTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		switch call {
		case 1:
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_0\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"hello.txt\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
		case 2:
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{}}]}\n\n")
			// Simulate NVIDIA dropping the continuation after receiving the tool result.
		case 3:
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			t.Fatalf("unexpected provider request %d", call)
		}
	}))
	defer srv.Close()

	events, result := runProvider(t, newTestProvider(t, srv), providers.RunRequest{RunID: "retry-after-tool", ProjectID: "p1", WorkingDirectory: dir, Prompt: "read", Model: "m"})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if got := len(findEvents(events, "tool", "tool.call")); got != 1 {
		t.Fatalf("tool calls=%d, want exactly 1", got)
	}
	if got := len(findEvents(events, "status", "openrouter.retry")); got != 1 {
		t.Fatalf("retry events=%d, want 1", got)
	}
	if calls.Load() != 3 {
		t.Fatalf("provider calls=%d, want 3", calls.Load())
	}
}

func TestAgentLoop_TextAroundToolCallRemainsSeparateTurns(t *testing.T) {
	store := newFakeConversationStore()
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{
				{Choices: []wireChoice{{Delta: wireDelta{Content: "Checking files."}}}},
				{Choices: []wireChoice{{
					Delta:        wireDelta{ToolCalls: []wireToolCallDelta{{Index: 0, ID: "call-1", Type: "function", Function: wireFunctionDelta{Name: "list_dir", Arguments: `{}`}}}},
					FinishReason: "tool_calls",
				}}},
			}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "The task is complete."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	events, result := runProvider(t, provider, providers.RunRequest{RunID: "turns", ProjectID: "p1", ThreadID: "thread", WorkingDirectory: t.TempDir(), Prompt: "go", Model: "m"})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	finals := findEvents(events, "message", "openrouter.message")
	if len(finals) != 2 {
		t.Fatalf("final messages = %d: %+v", len(finals), events)
	}
	first := finals[0].Payload.(map[string]any)
	second := finals[1].Payload.(map[string]any)
	if first["turn"] != 1 || second["turn"] != 2 {
		t.Fatalf("turn payloads = %+v %+v", first, second)
	}
	persisted, err := store.Load(context.Background(), "thread")
	if err != nil {
		t.Fatal(err)
	}
	var assistant []string
	for _, message := range persisted {
		if message.Role == "assistant" {
			assistant = append(assistant, message.Content)
		}
	}
	if len(assistant) != 2 || assistant[0] != "Checking files." || assistant[1] != "The task is complete." {
		t.Fatalf("assistant history = %+v", assistant)
	}
}

func TestAgentLoop_MultipleToolCallsInOneTurn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("content-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("content-b"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{
				Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
					{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
					{Index: 1, ID: "call_1", Type: "function", Function: wireFunctionDelta{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
				}}, FinishReason: "tool_calls"}},
			}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "Both files read."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "run3", ProjectID: "p1", WorkingDirectory: dir, Prompt: "read both files", Model: "test-model"}

	events, result := runProvider(t, provider, req)

	calls := findEvents(events, "tool", "tool.call")
	results := findEvents(events, "tool", "tool.result")
	if len(calls) != 2 || len(results) != 2 {
		t.Fatalf("want 2 tool.call + 2 tool.result, got %d/%d", len(calls), len(results))
	}
	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	body1 := log.body(1)
	msgs, _ := body1["messages"].([]any)
	toolMsgCount := 0
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" {
			toolMsgCount++
		}
	}
	if toolMsgCount != 2 {
		t.Errorf("second request carried %d tool messages, want 2", toolMsgCount)
	}
}

func TestAgentLoop_MalformedToolArguments(t *testing.T) {
	dir := t.TempDir()
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{
				Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
					{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "read_file", Arguments: `{not json`}},
				}}, FinishReason: "tool_calls"}},
			}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "Handled the bad arguments."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "run4", ProjectID: "p1", WorkingDirectory: dir, Prompt: "go", Model: "test-model"}

	events, result := runProvider(t, provider, req)

	if len(findEvents(events, "tool", "tool.call")) != 0 {
		t.Fatal("malformed tool call reached execution")
	}
	if result.Err == nil {
		t.Fatalf("result = %+v, want malformed response failure", result)
	}
}

func TestAgentLoop_DeniedTool(t *testing.T) {
	dir := t.TempDir()
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{
				Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
					{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "create_file", Arguments: `{"path":"x.txt","content":"y"}`}},
				}}, FinishReason: "tool_calls"}},
			}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "Could not write; read-only."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "run5", ProjectID: "p1", WorkingDirectory: dir, Prompt: "create a file", Model: "test-model", PermissionProfile: "read-only"}

	events, result := runProvider(t, provider, req)

	results := findEvents(events, "tool", "tool.result")
	if len(results) != 1 {
		t.Fatalf("want 1 tool.result, got %d", len(results))
	}
	payload, _ := results[0].Payload.(map[string]any)
	if payload["isError"] != true || !strings.Contains(fmt.Sprint(payload["result"]), "unknown or unavailable tool") {
		t.Errorf("expected denied-tool controlled result, got %+v", payload)
	}
	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Errorf("x.txt should not have been created under read-only profile")
	}
}

func TestAgentLoop_RepeatedIdenticalToolCall(t *testing.T) {
	dir := t.TempDir()
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{
			Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
				{Index: 0, ID: fmt.Sprintf("call_%d", call), Type: "function", Function: wireFunctionDelta{Name: "list_dir", Arguments: `{}`}},
			}}, FinishReason: "tool_calls"}},
		}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "run6", ProjectID: "p1", WorkingDirectory: dir, Prompt: "loop forever", Model: "test-model"}

	done := make(chan struct{})
	var events []providers.Event
	var result providers.Result
	go func() {
		events, result = runProvider(t, provider, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("run did not terminate; repeated tool call loop did not bound itself")
	}

	if result.Err != nil {
		t.Fatalf("result = %+v, want a clean finalize", result)
	}
	if log.count() > 10 {
		t.Fatalf("expected the loop to stop quickly after repeats, got %d HTTP calls", log.count())
	}
	results := findEvents(events, "tool", "tool.result")
	stopped := false
	for _, e := range results {
		payload, _ := e.Payload.(map[string]any)
		if strings.Contains(fmt.Sprint(payload["result"]), "repeated too many times") {
			stopped = true
		}
	}
	if !stopped {
		t.Errorf("expected a tool.result noting the repeated call was stopped")
	}
}

func TestAgentLoop_MaxTurns(t *testing.T) {
	dir := t.TempDir()
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{
			Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
				{Index: 0, ID: fmt.Sprintf("call_%d", call), Type: "function", Function: wireFunctionDelta{Name: "list_dir", Arguments: fmt.Sprintf(`{"path":"."}`)}},
			}}, FinishReason: "tool_calls"}},
		}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "run7", ProjectID: "p1", WorkingDirectory: dir, Prompt: "loop forever", Model: "test-model", MaxTurns: 3}

	done := make(chan struct{})
	var result providers.Result
	go func() {
		_, result = runProvider(t, provider, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("run did not terminate at MaxTurns")
	}

	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %+v, want a clean finalize", result)
	}
	if log.count() > req.MaxTurns+2 {
		t.Fatalf("expected roughly MaxTurns+1 HTTP calls, got %d", log.count())
	}
}

func TestAgentLoop_ExceedingToolCallBudgetStopsExecutingFurtherCalls(t *testing.T) {
	dir := t.TempDir()
	const totalCalls = maxToolCalls + 1

	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			deltas := make([]wireToolCallDelta, totalCalls)
			for i := 0; i < totalCalls; i++ {
				deltas[i] = wireToolCallDelta{
					Index: i,
					ID:    fmt.Sprintf("call_%d", i),
					Type:  "function",
					Function: wireFunctionDelta{
						Name:      "create_file",
						Arguments: fmt.Sprintf(`{"path":"f%d.txt","content":"x"}`, i),
					},
				}
			}
			return []wireChunk{{
				Choices: []wireChoice{{Delta: wireDelta{ToolCalls: deltas}, FinishReason: "tool_calls"}},
			}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "Stopped after budget exceeded."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "run-budget", ProjectID: "p1", WorkingDirectory: dir, Prompt: "spam tool calls", Model: "test-model"}

	events, result := runProvider(t, provider, req)

	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}

	for i := 0; i < maxToolCalls; i++ {
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("f%d.txt", i))); err != nil {
			t.Errorf("expected f%d.txt to exist (within budget), got: %v", i, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("f%d.txt", maxToolCalls))); err == nil {
		t.Errorf("f%d.txt should not have been created; its tool call exceeded the budget", maxToolCalls)
	}

	results := findEvents(events, "tool", "tool.result")
	if len(results) != totalCalls {
		t.Fatalf("want %d tool.result events, got %d", totalCalls, len(results))
	}
	last := results[len(results)-1]
	payload, _ := last.Payload.(map[string]any)
	if payload["isError"] != true || !strings.Contains(fmt.Sprint(payload["result"]), "tool call budget exceeded") {
		t.Errorf("expected the over-budget call to report a controlled budget-exceeded error, got %+v", payload)
	}
}

func TestAgentLoop_CancellationDoesNotRequireEventDrain(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := 0; i < 40; i++ {
			chunk, _ := json.Marshal(wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "x"}}}})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "cancel-no-drain", ProjectID: "p1", WorkingDirectory: t.TempDir(), Prompt: "go", Model: "m"}
	handle, err := provider.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := provider.Cancel(context.Background(), req.RunID); err != nil {
		t.Fatal(err)
	}
	done := make(chan providers.Result, 1)
	go func() { done <- handle.Wait() }()
	select {
	case result := <-done:
		if result.Err == nil {
			t.Fatalf("result=%+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled run blocked on an undrained event channel")
	}
}

func TestAgentLoop_CancellationDuringTool(t *testing.T) {
	if _, err := exec.LookPath("ping"); err != nil {
		t.Skipf("ping executable not available: %v", err)
	}
	dir := t.TempDir()
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{
			Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
				{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "run_command", Arguments: `{"command":"ping","args":["-n","30","127.0.0.1"]}`}},
			}}, FinishReason: "tool_calls"}},
		}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "run8", ProjectID: "p1", WorkingDirectory: dir, Prompt: "run a long command", Model: "test-model", PermissionProfile: "danger-full-access"}

	h, err := provider.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	sawToolCall := make(chan struct{})
	var once sync.Once
	go func() {
		for e := range h.Events() {
			if e.Type == "tool" && e.RawType == "tool.call" {
				once.Do(func() { close(sawToolCall) })
			}
		}
	}()

	select {
	case <-sawToolCall:
	case <-time.After(10 * time.Second):
		t.Fatal("did not observe the tool.call event for run_command")
	}

	if err := provider.Cancel(context.Background(), req.RunID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	start := time.Now()
	result := h.Wait()
	elapsed := time.Since(start)
	if elapsed > 10*time.Second {
		t.Fatalf("cancellation took too long: %v", elapsed)
	}
	if result.Err == nil {
		t.Fatalf("expected a context error after cancellation, got nil (result=%+v)", result)
	}
}

func TestAgentLoop_CancellationMidBatchPersistsToolCallsAndResults(t *testing.T) {
	if _, err := exec.LookPath("ping"); err != nil {
		t.Skipf("ping executable not available: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("content-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeConversationStore()
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{
			Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
				{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
				{Index: 1, ID: "call_1", Type: "function", Function: wireFunctionDelta{Name: "run_command", Arguments: `{"command":"ping","args":["-n","30","127.0.0.1"]}`}},
			}}, FinishReason: "tool_calls"}},
		}}
	})
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	req := providers.RunRequest{RunID: "run-cancel-batch", ProjectID: "p1", ThreadID: "thread-cancel-batch", WorkingDirectory: dir, Prompt: "read then run", Model: "test-model", PermissionProfile: "danger-full-access"}

	h, err := provider.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	sawSlowToolCall := make(chan struct{})
	var once sync.Once
	go func() {
		for e := range h.Events() {
			if e.Type == "tool" && e.RawType == "tool.call" {
				if payload, ok := e.Payload.(map[string]any); ok && payload["tool"] == "run_command" {
					once.Do(func() { close(sawSlowToolCall) })
				}
			}
		}
	}()

	select {
	case <-sawSlowToolCall:
	case <-time.After(10 * time.Second):
		t.Fatal("did not observe the tool.call event for run_command")
	}

	if err := provider.Cancel(context.Background(), req.RunID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	start := time.Now()
	result := h.Wait()
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("cancellation took too long: %v", elapsed)
	}
	if result.Err == nil {
		t.Fatalf("expected a context error after cancellation, got nil (result=%+v)", result)
	}

	store.mu.Lock()
	persisted := append([]StoredMessage{}, store.byID["thread-cancel-batch"]...)
	store.mu.Unlock()

	sawAssistantToolCalls := false
	sawFirstToolResult := false
	for _, m := range persisted {
		if m.Role == "assistant" && strings.Contains(m.ToolCallsJSON, "call_0") && strings.Contains(m.ToolCallsJSON, "call_1") {
			sawAssistantToolCalls = true
		}
		if m.Role == "tool" && m.ToolCallID == "call_0" && strings.Contains(m.Content, "content-a") {
			sawFirstToolResult = true
		}
	}
	if !sawAssistantToolCalls {
		t.Errorf("expected the assistant tool_calls message to be persisted despite mid-batch cancellation: %+v", persisted)
	}
	if !sawFirstToolResult {
		t.Errorf("expected the already-executed tool result to be persisted despite mid-batch cancellation: %+v", persisted)
	}
}

func TestAgentLoop_BudgetExhaustion(t *testing.T) {
	dir := t.TempDir()
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{
				Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
					{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "list_dir", Arguments: `{}`}},
				}}, FinishReason: "tool_calls"}},
				Usage: &wireUsage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60, Cost: 0.02},
			}}
		}
		return []wireChunk{{
			Choices: []wireChoice{{Delta: wireDelta{Content: "Done within budget."}, FinishReason: "stop"}},
			Usage:   &wireUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, Cost: 0.001},
		}}
	})
	provider := newTestProvider(t, srv)
	provider.SetModelInfo(func(string) (ModelInfo, bool) {
		return ModelInfo{Tools: true, Verified: true, PriceKnown: true, CompletionPrice: 0.0001}, true
	})
	req := providers.RunRequest{RunID: "run9", ProjectID: "p1", WorkingDirectory: dir, Prompt: "spend a bit", Model: "test-model", MaxBudget: 0.01}

	_, result := runProvider(t, provider, req)

	if log.count() != 1 {
		t.Fatalf("want exactly 1 HTTP call with no over-budget final request, got %d", log.count())
	}
	maxTokens, ok := log.body(0)["max_tokens"].(float64)
	if !ok || maxTokens < minimumBudgetedOutputTokens || maxTokens > 100 {
		t.Fatalf("max_tokens=%v, want a budget-derived request cap", log.body(0)["max_tokens"])
	}
	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if result.Cost < 0.02 {
		t.Errorf("result cost = %v, want at least 0.02", result.Cost)
	}
}

func TestAgentLoop_BudgetStopsWhenUsageIsMissing(t *testing.T) {
	dir := t.TempDir()
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{
			Delta:        wireDelta{ToolCalls: []wireToolCallDelta{{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "list_dir", Arguments: `{}`}}}},
			FinishReason: "tool_calls",
		}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetModelInfo(func(string) (ModelInfo, bool) {
		return ModelInfo{Tools: true, Verified: true, PriceKnown: true, CompletionPrice: 0.0001}, true
	})
	_, result := runProvider(t, provider, providers.RunRequest{RunID: "run-budget-no-usage", ProjectID: "p1", WorkingDirectory: dir, Prompt: "inspect", Model: "test-model", MaxBudget: 0.01})
	if log.count() != 1 {
		t.Fatalf("HTTP calls=%d, want 1 after missing usage", log.count())
	}
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("result=%+v", result)
	}
}

func TestAgentLoop_RecordsActualFallbackModel(t *testing.T) {
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{
			Model:   "vendor/fallback",
			Choices: []wireChoice{{Delta: wireDelta{Content: "done"}, FinishReason: "stop"}},
			Usage:   &wireUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, Cost: 0.002},
		}}
	})
	store := newFakeConversationStore()
	provider := newTestProvider(t, srv)
	provider.SetConversationStore(store)
	provider.SetModelInfo(func(id string) (ModelInfo, bool) {
		return ModelInfo{Tools: true, Verified: true, PriceKnown: true, PromptPrice: 0.000001, CompletionPrice: 0.000002}, true
	})
	events, result := runProvider(t, provider, providers.RunRequest{RunID: "run-fallback", ProjectID: "p1", ThreadID: "thread-fallback", WorkingDirectory: t.TempDir(), Prompt: "go", Model: "vendor/requested"})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	usageEvents := findEvents(events, "usage", "openrouter.usage")
	if len(usageEvents) != 1 || usageEvents[0].Payload.(map[string]any)["model"] != "vendor/fallback" {
		t.Fatalf("usage events=%+v", usageEvents)
	}
	store.mu.Lock()
	persisted := append([]StoredMessage(nil), store.byID["thread-fallback"]...)
	store.mu.Unlock()
	if len(persisted) < 2 || persisted[len(persisted)-1].Model != "vendor/fallback" {
		t.Fatalf("persisted=%+v", persisted)
	}
}

func TestBudgetedMaxTokens(t *testing.T) {
	messages := []orclient.Message{{Role: "user", Content: "hello"}}
	priced := ModelInfo{PriceKnown: true, PromptPrice: 0.000001, CompletionPrice: 0.00001}
	tests := []struct {
		name    string
		limit   float64
		spent   float64
		info    ModelInfo
		allowed bool
	}{
		{"already exhausted", 0.01, 0.01, priced, false},
		{"below minimum response", 0.0002, 0, priced, false},
		{"near ceiling", 0.01, 0.009, priced, true},
		{"unknown price", 0.01, 0, ModelInfo{}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, allowed := budgetedMaxTokens(test.limit, test.spent, test.info, true, messages, nil)
			if allowed != test.allowed {
				t.Fatalf("allowed=%v want=%v", allowed, test.allowed)
			}
		})
	}
}

func TestAgentLoop_LocalFileEdit(t *testing.T) {
	dir := t.TempDir()
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{
				Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
					{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "create_file", Arguments: `{"path":"new.txt","content":"created by tool"}`}},
				}}, FinishReason: "tool_calls"}},
			}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "File created."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "run10", ProjectID: "p1", WorkingDirectory: dir, Prompt: "create a file", Model: "test-model"}

	_, result := runProvider(t, provider, req)
	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatalf("expected new.txt to exist: %v", err)
	}
	if string(data) != "created by tool" {
		t.Errorf("new.txt content = %q, want %q", data, "created by tool")
	}
}

func TestAgentLoop_ShellTimeout(t *testing.T) {
	t.Skip("no seam to configure a short CommandTimeout on the provider without expanding its public API beyond the Phase C2 surface (SetBaseURL/SetKeySource only); exercising the real default (120s) is impractical in a fast test suite")
}
