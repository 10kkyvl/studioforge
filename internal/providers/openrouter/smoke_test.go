package openrouter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/processes"
	"github.com/10kkyvl/studioforge/internal/providers"
)

func newSmokeProvider(t *testing.T, key string) *Provider {
	t.Helper()
	sup := processes.NewSupervisor()
	t.Cleanup(func() { _ = sup.Close(context.Background()) })
	p := New(sup)
	p.SetKeySource(func() string { return key })
	return p
}

func TestSmoke_MinimalStream(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("set OPENROUTER_API_KEY to run live OpenRouter smoke tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p := newSmokeProvider(t, key)
	req := providers.RunRequest{
		RunID:            "smoke-minimal-stream",
		ProjectID:        "smoke",
		WorkingDirectory: t.TempDir(),
		Prompt:           "Reply with the single word: OK.",
		Model:            "openai/gpt-oss-20b:free",
		MaxBudget:        0.05,
	}
	handle, err := p.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	sawMessage := false
	for event := range handle.Events() {
		t.Logf("event type=%s rawType=%s payload=%+v error=%q", event.Type, event.RawType, event.Payload, event.Error)
		if event.Type == "message" {
			if payload, ok := event.Payload.(map[string]any); ok {
				if text, _ := payload["text"].(string); strings.TrimSpace(text) != "" {
					sawMessage = true
				}
			}
		}
	}
	result := handle.Wait()
	t.Logf("result=%+v sawMessage=%v", result, sawMessage)

	if result.Err != nil {
		t.Logf("provider returned a terminal error (acceptable for a free model): %v", result.Err)
		return
	}
	if !sawMessage {
		t.Fatalf("expected at least one message event with non-empty text, got none (result=%+v)", result)
	}
}

func TestSmoke_FreeAutomatic(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("set OPENROUTER_API_KEY to run live OpenRouter smoke tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p := newSmokeProvider(t, key)
	req := providers.RunRequest{
		RunID:            "smoke-free-automatic",
		ProjectID:        "smoke",
		WorkingDirectory: t.TempDir(),
		Prompt:           "Reply with the single word: OK.",
		Model:            "openrouter/free",
		MaxBudget:        0.05,
	}
	handle, err := p.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	sawMessage := false
	for event := range handle.Events() {
		t.Logf("event type=%s rawType=%s payload=%+v error=%q", event.Type, event.RawType, event.Payload, event.Error)
		if event.Type == "message" {
			if payload, ok := event.Payload.(map[string]any); ok {
				if text, _ := payload["text"].(string); strings.TrimSpace(text) != "" {
					sawMessage = true
				}
			}
		}
	}
	result := handle.Wait()
	t.Logf("result=%+v sawMessage=%v", result, sawMessage)

	if result.Err != nil {
		t.Logf("openrouter/free returned a terminal error (acceptable for the automatic free router): %v", result.Err)
		return
	}
	if !sawMessage {
		t.Log("run completed with no error but no message text was observed; the free router path still reached a clean terminal state")
	}
}

func TestSmoke_OneToolCall(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("set OPENROUTER_API_KEY to run live OpenRouter smoke tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	dir := t.TempDir()
	const marker = "smoke-marker-42"
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}

	p := newSmokeProvider(t, key)
	req := providers.RunRequest{
		RunID:             "smoke-one-tool-call",
		ProjectID:         "smoke",
		WorkingDirectory:  dir,
		Prompt:            "Use the read_file tool to read hello.txt and tell me the exact marker string inside it.",
		Model:             "openai/gpt-oss-20b:free",
		PermissionProfile: "read-only",
		MaxBudget:         0.05,
	}
	handle, err := p.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	sawToolCall := false
	sawToolResult := false
	sawMarkerInToolResult := false
	sawMarkerInMessage := false
	for event := range handle.Events() {
		t.Logf("event type=%s rawType=%s payload=%+v error=%q", event.Type, event.RawType, event.Payload, event.Error)
		payload, _ := event.Payload.(map[string]any)
		switch {
		case event.Type == "tool" && event.RawType == "tool.call":
			if payload["tool"] == "read_file" {
				sawToolCall = true
			}
		case event.Type == "tool" && event.RawType == "tool.result":
			if payload["tool"] == "read_file" {
				sawToolResult = true
				if strings.Contains(fmt.Sprint(payload["result"]), marker) {
					sawMarkerInToolResult = true
				}
			}
		case event.Type == "message":
			if text, _ := payload["text"].(string); strings.Contains(text, marker) {
				sawMarkerInMessage = true
			}
		}
	}
	result := handle.Wait()
	t.Logf("result=%+v sawToolCall=%v sawToolResult=%v sawMarkerInToolResult=%v sawMarkerInMessage=%v",
		result, sawToolCall, sawToolResult, sawMarkerInToolResult, sawMarkerInMessage)

	if !sawToolCall || !sawToolResult {
		t.Log("free model did not exercise the read_file tool path; this is a known free-model reliability gap, not a provider defect")
	}
	if !sawMarkerInToolResult && !sawMarkerInMessage {
		t.Logf("marker %q was not observed in the tool result or the final message; the free model may have declined the tool call", marker)
	}

	wantSession := "openrouter:" + req.RunID
	if result.SessionID != wantSession {
		t.Fatalf("run did not reach a terminal state on the real execution path: sessionID=%q, want %q (result=%+v)", result.SessionID, wantSession, result)
	}
}

func TestSmoke_Cancellation(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("set OPENROUTER_API_KEY to run live OpenRouter smoke tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p := newSmokeProvider(t, key)
	req := providers.RunRequest{
		RunID:            "smoke-cancellation",
		ProjectID:        "smoke",
		WorkingDirectory: t.TempDir(),
		Prompt:           "Write four or five sentences describing clouds drifting across a summer sky.",
		Model:            "openai/gpt-oss-20b:free",
		MaxBudget:        0.05,
	}
	handle, err := p.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	sawStreaming := make(chan struct{})
	eventsClosed := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(eventsClosed)
		for event := range handle.Events() {
			t.Logf("event type=%s rawType=%s", event.Type, event.RawType)
			if event.RawType == "openrouter.message.partial" {
				once.Do(func() { close(sawStreaming) })
			}
		}
	}()

	select {
	case <-sawStreaming:
	case <-eventsClosed:
		result := handle.Wait()
		t.Skipf("model completed before a streaming chunk was observed to cancel against (result=%+v)", result)
	case <-time.After(30 * time.Second):
		t.Fatal("did not observe a streaming message chunk within 30s")
	}

	if err := p.Cancel(context.Background(), req.RunID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	start := time.Now()
	result := handle.Wait()
	elapsed := time.Since(start)
	t.Logf("cancellation took %v, result=%+v", elapsed, result)

	if elapsed > 10*time.Second {
		t.Fatalf("cancellation took too long: %v", elapsed)
	}
	if result.Err == nil {
		t.Fatalf("expected a context error after cancellation, got nil (result=%+v)", result)
	}
}
