package scheduler

import (
	"context"
	"testing"

	"github.com/10kkyvl/studioforge/internal/providers"
)

// mutationTestStore is a RunStore that only needs SetRunStudioDirectEdits to
// work: embedding the nil interface promotes every other method, and none of
// them are exercised by trackStudioMutation.
type mutationTestStore struct {
	RunStore
	flaggedRunID string
	callCount    int
}

func (s *mutationTestStore) SetRunStudioDirectEdits(ctx context.Context, id string) error {
	s.flaggedRunID = id
	s.callCount++
	return nil
}

// studioMutationChecker stands in for mcp.MutatesPlace without importing that
// package (internal/scheduler must stay provider/Roblox-neutral): only the
// tool names these tests actually use are classified.
func studioMutationChecker(name string) bool {
	return name == "mcp__Roblox_Studio__multi_edit"
}

func TestClaudeMutatingToolCallFlagsStudioDirectEdits(t *testing.T) {
	store := &mutationTestStore{}
	m := &Manager{store: store, mutationChecker: studioMutationChecker}
	e := &execution{job: &Job{RunID: "run-1", ProjectID: "proj-1"}}
	event := providers.Event{Type: "message", RawType: "assistant", Payload: claudeToolUse("mcp__Roblox_Studio__multi_edit", map[string]any{})}

	m.trackStudioMutation(e, event)

	if !e.studioDirectEdits {
		t.Fatal("a Claude multi_edit tool call must flag the run as a Studio direct edit")
	}
	if store.flaggedRunID != "run-1" || store.callCount != 1 {
		t.Fatalf("SetRunStudioDirectEdits was not persisted for the flagged run: flaggedRunID=%q callCount=%d", store.flaggedRunID, store.callCount)
	}
}

func TestAgentLoopMutatingToolCallFlagsStudioDirectEdits(t *testing.T) {
	store := &mutationTestStore{}
	m := &Manager{store: store, mutationChecker: studioMutationChecker}
	e := &execution{job: &Job{RunID: "run-2", ProjectID: "proj-1"}}

	m.trackStudioMutation(e, agentLoopCall("mcp__Roblox_Studio__multi_edit"))

	if !e.studioDirectEdits {
		t.Fatal("an OpenRouter/NVIDIA tool.call for multi_edit must flag the run as a Studio direct edit")
	}
	if store.flaggedRunID != "run-2" || store.callCount != 1 {
		t.Fatalf("SetRunStudioDirectEdits was not persisted for the flagged run: flaggedRunID=%q callCount=%d", store.flaggedRunID, store.callCount)
	}
}

func TestReadOnlyStudioToolDoesNotFlagStudioDirectEdits(t *testing.T) {
	store := &mutationTestStore{}
	m := &Manager{store: store, mutationChecker: studioMutationChecker}
	e := &execution{job: &Job{RunID: "run-3", ProjectID: "proj-1"}}
	event := providers.Event{Type: "message", RawType: "assistant", Payload: claudeToolUse("mcp__Roblox_Studio__script_read", map[string]any{})}

	m.trackStudioMutation(e, event)

	if e.studioDirectEdits {
		t.Fatal("a read-only Studio tool call must not flag the run")
	}
	if store.callCount != 0 {
		t.Fatalf("nothing should have been persisted, callCount=%d", store.callCount)
	}
}

// This is the regression guard for the stuck-detection trap: trackStudioMutation
// is called unconditionally from run()'s event loop, never gated behind
// j.StuckDetectionEnabled the way trackStuckSignals is, because an agent's own
// per-run stuck-detection opt-out has nothing to do with whether it changed
// Studio directly. Driving the flag through with StuckDetectionEnabled: false
// set on the job proves the two are independent.
func TestStudioMutationFlagsEvenWhenStuckDetectionIsDisabled(t *testing.T) {
	store := &mutationTestStore{}
	m := &Manager{store: store, mutationChecker: studioMutationChecker}
	e := &execution{job: &Job{RunID: "run-4", ProjectID: "proj-1", StuckDetectionEnabled: false}}
	event := providers.Event{Type: "message", RawType: "assistant", Payload: claudeToolUse("mcp__Roblox_Studio__multi_edit", map[string]any{})}

	m.trackStudioMutation(e, event)

	if !e.studioDirectEdits {
		t.Fatal("the Studio-direct-edit flag must not depend on StuckDetectionEnabled")
	}
}

func TestStudioMutationPersistsOnlyOnce(t *testing.T) {
	store := &mutationTestStore{}
	m := &Manager{store: store, mutationChecker: studioMutationChecker}
	e := &execution{job: &Job{RunID: "run-5", ProjectID: "proj-1"}}
	event := providers.Event{Type: "message", RawType: "assistant", Payload: claudeToolUse("mcp__Roblox_Studio__multi_edit", map[string]any{})}

	m.trackStudioMutation(e, event)
	m.trackStudioMutation(e, event)

	if store.callCount != 1 {
		t.Fatalf("SetRunStudioDirectEdits should persist once, on the first flip: callCount=%d", store.callCount)
	}
}
