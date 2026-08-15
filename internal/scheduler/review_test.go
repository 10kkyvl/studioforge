package scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/resources"
)

func newReviewHarness(t *testing.T, ttl time.Duration, provider providers.Provider) (*Manager, *database.Store, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := database.NewStore(db)
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub(store)
	t.Cleanup(hub.Close)
	leases := resources.NewManager(ttl)
	t.Cleanup(leases.Close)
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider, "claude": provider})
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return manager, store, ctx
}

type reviewOpenCall struct {
	runID, projectID, checkpoint string
	expires                      time.Time
}

type recordingReviewGate struct {
	store *database.Store
	mu    sync.Mutex
	calls []reviewOpenCall
	err   error
}

func (g *recordingReviewGate) Open(ctx context.Context, runID, projectID, checkpoint string, expires time.Time) error {
	if g.err != nil {
		return g.err
	}
	g.mu.Lock()
	g.calls = append(g.calls, reviewOpenCall{runID, projectID, checkpoint, expires})
	g.mu.Unlock()
	_, err := g.store.CreateReview(ctx, models.RunReview{RunID: runID, ProjectID: projectID, CheckpointHash: checkpoint, ExpiresAt: expires})
	return err
}

func (g *recordingReviewGate) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

func editStep() providers.Event {
	return providers.Event{Type: "message", RawType: "assistant", Payload: claudeToolUse("Edit", map[string]any{"file_path": "script.lua"})}
}

func readOnlyStep() providers.Event {
	return providers.Event{Type: "message", RawType: "assistant", Payload: claudeToolUse("Read", map[string]any{"file_path": "script.lua"})}
}

func TestReviewGateParksTheRunInWaitingDecision(t *testing.T) {
	provider := &stuckStreamProvider{steps: []providers.Event{editStep()}, stepDelay: time.Millisecond}
	manager, store, ctx := newReviewHarness(t, time.Second, provider)
	gate := &recordingReviewGate{store: store}
	manager.SetReviewGate(gate)

	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1, ReviewBeforeApply: true, BaseCheckpoint: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "waiting_decision", 3*time.Second)

	if gate.callCount() != 1 {
		t.Fatalf("Open call count=%d, want 1", gate.callCount())
	}
	call := gate.calls[0]
	if call.runID != run.ID || call.projectID != "demo-obby" || call.checkpoint != "abc123" {
		t.Fatalf("Open call=%+v", call)
	}
	if !call.expires.After(time.Now()) {
		t.Fatalf("Open call expires=%v, want a future time", call.expires)
	}

	found := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !found {
		list, err := store.EventsAfter(ctx, 0, "demo-obby", run.ID, 200)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range list {
			if event.Type == "review" {
				found = true
				break
			}
		}
		if !found {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !found {
		t.Fatal("no review event was published when the review gate opened")
	}
}

func TestReviewGateIsSkippedWhenTheAgentDidNotOptIn(t *testing.T) {
	provider := &stuckStreamProvider{steps: []providers.Event{editStep()}, stepDelay: time.Millisecond}
	manager, store, ctx := newReviewHarness(t, time.Second, provider)
	gate := &recordingReviewGate{store: store}
	manager.SetReviewGate(gate)

	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "completed", 3*time.Second)
	if gate.callCount() != 0 {
		t.Fatalf("Open call count=%d, want 0 when the agent did not opt in", gate.callCount())
	}
}

func TestReviewGateIsSkippedWhenTheRunChangedNothing(t *testing.T) {
	provider := &stuckStreamProvider{steps: []providers.Event{readOnlyStep()}, stepDelay: time.Millisecond}
	manager, store, ctx := newReviewHarness(t, time.Second, provider)
	gate := &recordingReviewGate{store: store}
	manager.SetReviewGate(gate)

	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1, ReviewBeforeApply: true, BaseCheckpoint: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "completed", 3*time.Second)
	if gate.callCount() != 0 {
		t.Fatalf("Open call count=%d, want 0 when the run made no file edits", gate.callCount())
	}
}

func TestReviewGateHoldsTheProjectWriteLease(t *testing.T) {
	provider := &stuckStreamProvider{steps: []providers.Event{editStep()}, stepDelay: time.Millisecond}
	manager, store, ctx := newReviewHarness(t, 2*time.Second, provider)
	gate := &recordingReviewGate{store: store}
	manager.SetReviewGate(gate)

	first, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1, ReviewBeforeApply: true, BaseCheckpoint: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, first.ID, "waiting_decision", 3*time.Second)

	second, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-eng", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		run, err := store.Run(ctx, second.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == "running" || run.Status == "completed" {
			t.Fatalf("second run reached status=%q while the review still held the project lease", run.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := manager.Cancel(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReviewGateFailsTheRunWhenTheLeaseCannotBeHeld(t *testing.T) {
	provider := &stuckStreamProvider{
		steps: []providers.Event{
			editStep(), editStep(), editStep(), editStep(), editStep(), editStep(),
		},
		stepDelay: 40 * time.Millisecond,
	}
	manager, store, ctx := newReviewHarness(t, 20*time.Millisecond, provider)
	manager.tick = 5 * time.Second
	gate := &recordingReviewGate{store: store}
	manager.SetReviewGate(gate)

	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1, ReviewBeforeApply: true, BaseCheckpoint: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "failed", 5*time.Second)
	if gate.callCount() != 0 {
		t.Fatalf("Open call count=%d, want 0: Open must never be reached once the lease transfer itself failed", gate.callCount())
	}
}
