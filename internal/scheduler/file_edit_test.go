package scheduler

import (
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/providers"
)

func TestClaudeFileEditToolCallPublishesTransientFileEditEvent(t *testing.T) {
	hub := events.NewHub(nil)
	defer hub.Close()
	stream, cancel := hub.Subscribe(4)
	defer cancel()
	m := &Manager{hub: hub}
	e := &execution{job: &Job{RunID: "run-1", ProjectID: "proj-1", AgentID: "agent-1"}}
	event := providers.Event{Type: "message", RawType: "assistant", Payload: claudeToolUse("Edit", map[string]any{})}

	m.trackFileEdit(e, event)

	select {
	case got := <-stream:
		if got.Type != "file_edit" || got.RawType != "scheduler.file_edit" {
			t.Fatalf("event=%+v", got)
		}
		if got.RunID != "run-1" || got.ProjectID != "proj-1" || got.AgentID != "agent-1" {
			t.Fatalf("event=%+v", got)
		}
		payload, ok := got.Payload.(map[string]any)
		if !ok {
			t.Fatalf("payload type=%T", got.Payload)
		}
		tools, ok := payload["tools"].([]string)
		if !ok || len(tools) != 1 || tools[0] != "Edit" {
			t.Fatalf("tools=%v", payload["tools"])
		}
	case <-time.After(time.Second):
		t.Fatal("expected a transient file_edit event")
	}
}

func TestAgentLoopFileEditToolCallPublishesTransientFileEditEvent(t *testing.T) {
	hub := events.NewHub(nil)
	defer hub.Close()
	stream, cancel := hub.Subscribe(4)
	defer cancel()
	m := &Manager{hub: hub}
	e := &execution{job: &Job{RunID: "run-2", ProjectID: "proj-1", AgentID: "agent-1"}}

	m.trackFileEdit(e, agentLoopCall("apply_patch"))

	select {
	case got := <-stream:
		if got.Type != "file_edit" || got.RawType != "scheduler.file_edit" {
			t.Fatalf("event=%+v", got)
		}
		payload, ok := got.Payload.(map[string]any)
		if !ok {
			t.Fatalf("payload type=%T", got.Payload)
		}
		tools, ok := payload["tools"].([]string)
		if !ok || len(tools) != 1 || tools[0] != "apply_patch" {
			t.Fatalf("tools=%v", payload["tools"])
		}
	case <-time.After(time.Second):
		t.Fatal("expected a transient file_edit event")
	}
}

func TestReadOnlyToolCallsDoNotPublishFileEditEvent(t *testing.T) {
	hub := events.NewHub(nil)
	defer hub.Close()
	stream, cancel := hub.Subscribe(4)
	defer cancel()
	m := &Manager{hub: hub}
	e := &execution{job: &Job{RunID: "run-3", ProjectID: "proj-1", AgentID: "agent-1"}}

	m.trackFileEdit(e, providers.Event{Type: "message", RawType: "assistant", Payload: claudeToolUse("Read", map[string]any{})})
	m.trackFileEdit(e, agentLoopCall("get_console_output"))

	select {
	case got := <-stream:
		t.Fatalf("read-only tool calls must not publish a file_edit event, got %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}
