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

// stuckStreamHandle/stuckStreamProvider stream a fixed list of events, then —
// if loop is set — keep repeating from loopFrom forever (stepDelay apart)
// until Cancel is called, exactly like escalateStuck is expected to do once a
// threshold trips. This is the same shape as cancel_test.go's
// slowCancelProvider, just driven by caller-supplied events instead of a
// fixed "streaming" placeholder, so a test can control the exact Claude-shaped
// payloads the stuck-detection bookkeeping reads.
type stuckStreamProvider struct {
	steps     []providers.Event
	stepDelay time.Duration
	loop      bool
	loopFrom  int
	// hang keeps the handle open, emitting nothing, once steps run out —
	// the shape of a hung provider the idle check exists to catch.
	hang bool
}

func (p *stuckStreamProvider) Diagnose(context.Context) providers.Diagnostics {
	return providers.Diagnostics{Available: true, Authenticated: true}
}
func (p *stuckStreamProvider) Start(_ context.Context, _ providers.RunRequest) (providers.RunHandle, error) {
	h := &stuckStreamHandle{events: make(chan providers.Event, 64), done: make(chan struct{}), stop: make(chan struct{})}
	go h.stream(p)
	return h, nil
}
func (p *stuckStreamProvider) Resume(ctx context.Context, req providers.ResumeRequest) (providers.RunHandle, error) {
	return p.Start(ctx, req.RunRequest)
}
func (p *stuckStreamProvider) Cancel(context.Context, string) error { return nil }

type stuckStreamHandle struct {
	events chan providers.Event
	done   chan struct{}
	stop   chan struct{}
	once   sync.Once
	result providers.Result
}

func (h *stuckStreamHandle) stream(p *stuckStreamProvider) {
	defer close(h.events)
	defer close(h.done)
	index := 0
	for {
		var event providers.Event
		switch {
		case index < len(p.steps):
			event = p.steps[index]
			index++
		case p.loop && len(p.steps) > 0:
			event = p.steps[p.loopFrom]
			index = p.loopFrom + 1
		case p.hang:
			<-h.stop
			h.result = providers.Result{SessionID: "sess-stuck-stream", ExitCode: -1}
			return
		default:
			h.result = providers.Result{SessionID: "sess-stuck-stream", ExitCode: 0}
			return
		}
		event.At = time.Now().UTC()
		select {
		case <-h.stop:
			h.result = providers.Result{SessionID: "sess-stuck-stream", ExitCode: -1}
			return
		case h.events <- event:
		}
		delay := p.stepDelay
		if delay <= 0 {
			delay = time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-h.stop:
			timer.Stop()
			h.result = providers.Result{SessionID: "sess-stuck-stream", ExitCode: -1}
			return
		case <-timer.C:
		}
	}
}
func (h *stuckStreamHandle) Events() <-chan providers.Event { return h.events }
func (h *stuckStreamHandle) Wait() providers.Result         { <-h.done; return h.result }
func (h *stuckStreamHandle) Cancel() error {
	h.once.Do(func() { close(h.stop) })
	return nil
}

func newStuckHarness(t *testing.T, provider providers.Provider) (*Manager, *database.Store, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "stuck.db"))
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
	leases := resources.NewManager(time.Second)
	t.Cleanup(leases.Close)
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider, "claude": provider})
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return manager, store, ctx
}

// freshProjectAgent creates a brand-new project and agent (via the same store
// methods every other package already uses) so a test never fights the demo
// seed's own rows on "demo-obby". Foreign keys require a real project_agents
// row, so a fabricated id string is not an option here.
func freshProjectAgent(t *testing.T, store *database.Store, ctx context.Context, name string) (projectID, agentID string) {
	t.Helper()
	project, err := store.CreateProject(ctx, models.Project{Name: name, Path: t.TempDir(), Fingerprint: name})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.CreateAgent(ctx, models.Agent{ProjectID: project.ID, Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	return project.ID, agent.ID
}

// assertWaitingDecisionWithStuckFence waits for the run to land on
// waiting_decision and asserts one of its message events carries a valid
// studioforge-question fence with the single StuckContinueLabel option, and
// that the run's own stuck_escalated column was written.
func assertWaitingDecisionWithStuckFence(t *testing.T, store *database.Store, ctx context.Context, runID string) {
	t.Helper()
	waitStatus(t, store, runID, "waiting_decision", 5*time.Second)
	run, err := store.Run(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.StuckEscalated {
		t.Errorf("run %s: stuckEscalated=false, want true", runID)
	}
	list, err := store.EventsAfter(ctx, 0, "", runID, 500)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range list {
		if event.Type != "message" {
			continue
		}
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			continue
		}
		text, _ := payload["text"].(string)
		if text == "" {
			continue
		}
		block, ok := detectQuestion(text)
		if !ok {
			continue
		}
		if len(block.Options) == 1 && block.Options[0].Label == StuckContinueLabel {
			found = true
		}
		if event.RawType != "scheduler.stuck" {
			t.Errorf("stuck-escalation message event RawType=%q, want scheduler.stuck", event.RawType)
		}
	}
	if !found {
		t.Fatalf("no scheduler.stuck message event with a valid Continue-only question fence found for run %s", runID)
	}
}

func toolUseAssistantEvent(names ...string) providers.Event {
	blocks := make([]any, 0, len(names))
	for _, name := range names {
		blocks = append(blocks, map[string]any{"type": "tool_use", "name": name, "input": map[string]any{}})
	}
	return providers.Event{Type: "message", RawType: "assistant", Payload: map[string]any{
		"type":    "assistant",
		"message": map[string]any{"content": blocks},
	}}
}

func toolResultEvent(text string) providers.Event {
	return providers.Event{Type: "tool", RawType: "user", Payload: map[string]any{
		"type": "user",
		"message": map[string]any{"content": []any{
			map[string]any{"type": "tool_result", "content": text},
		}},
	}}
}

// TestStuckIdleThresholdTripsOnAHungProvider covers the idle check end to
// end: the provider emits one event, then hangs without completing, so the
// run loop's own ticker (shrunk via manager.tick) must notice the silence
// and escalate.
func TestStuckIdleThresholdTripsOnAHungProvider(t *testing.T) {
	provider := &stuckStreamProvider{
		steps: []providers.Event{{Type: "status", RawType: "mock.start", Payload: map[string]any{"message": "started"}}},
		hang:  true,
	}
	manager, store, ctx := newStuckHarness(t, provider)
	manager.tick = 50 * time.Millisecond
	projectID, agentID := freshProjectAgent(t, store, ctx, "stuck-idle-hung")
	run, _, err := manager.Submit(ctx, Job{
		ProjectID: projectID, AgentID: agentID, Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1,
		Prompt:                "Playtest the obby",
		StuckDetectionEnabled: true, StuckIdleSeconds: 1, StuckRepetitionCap: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertWaitingDecisionWithStuckFence(t, store, ctx, run.ID)
}

// TestStuckIdleDoesNotTripWhileEventsFlow is the idle check's own
// false-positive guard: a run that keeps producing events, however long it
// takes overall, must never be flagged idle — every event resets the anchor.
func TestStuckIdleDoesNotTripWhileEventsFlow(t *testing.T) {
	var steps []providers.Event
	for i := 0; i < 30; i++ {
		steps = append(steps, providers.Event{Type: "status", RawType: "mock.progress", Payload: map[string]any{"message": "still working"}})
	}
	provider := &stuckStreamProvider{steps: steps, stepDelay: 50 * time.Millisecond}
	manager, store, ctx := newStuckHarness(t, provider)
	manager.tick = 50 * time.Millisecond
	projectID, agentID := freshProjectAgent(t, store, ctx, "stuck-idle-flowing")
	run, _, err := manager.Submit(ctx, Job{
		ProjectID: projectID, AgentID: agentID, Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1,
		Prompt:                "Playtest the obby",
		StuckDetectionEnabled: true, StuckIdleSeconds: 1, StuckRepetitionCap: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "completed", 10*time.Second)
}

// TestStuckRepetitionThresholdTrips covers the repeated-tool-cycle check: the
// same two-tool cycle (a Studio MCP play/observe loop) repeats with identical
// console output every time, no file edit anywhere, so the distinct
// observation count never grows across the window.
func TestStuckRepetitionThresholdTrips(t *testing.T) {
	steps := []providers.Event{
		toolUseAssistantEvent("start_stop_play"),
		toolResultEvent("Play mode started"),
		toolUseAssistantEvent("get_console_output"),
		toolResultEvent("no errors"),
	}
	provider := &stuckStreamProvider{steps: steps, stepDelay: time.Millisecond, loop: true, loopFrom: 0}
	manager, store, ctx := newStuckHarness(t, provider)
	projectID, agentID := freshProjectAgent(t, store, ctx, "stuck-repetition")
	run, _, err := manager.Submit(ctx, Job{
		ProjectID: projectID, AgentID: agentID, Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1,
		Prompt:                "Playtest the obby",
		StuckDetectionEnabled: true, StuckIdleSeconds: 3600, StuckRepetitionCap: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertWaitingDecisionWithStuckFence(t, store, ctx, run.ID)
}

// TestStuckRepetitionDoesNotTripWhenObservationsKeepChanging is the
// repetition check's own false-positive guard: the same tool cycle repeats,
// but each cycle turns up a different console observation, so the agent is
// still learning something new each time and must not be flagged.
func TestStuckRepetitionDoesNotTripWhenObservationsKeepChanging(t *testing.T) {
	var steps []providers.Event
	for i := 0; i < 12; i++ {
		steps = append(steps,
			toolUseAssistantEvent("start_stop_play"),
			toolResultEvent("Play mode started"),
			toolUseAssistantEvent("get_console_output"),
			toolResultEvent(uniqueObservation(i)),
		)
	}
	provider := &stuckStreamProvider{steps: steps, stepDelay: time.Millisecond}
	manager, store, ctx := newStuckHarness(t, provider)
	projectID, agentID := freshProjectAgent(t, store, ctx, "stuck-repetition-progressing")
	run, _, err := manager.Submit(ctx, Job{
		ProjectID: projectID, AgentID: agentID, Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1,
		Prompt:                "Playtest the obby",
		StuckDetectionEnabled: true, StuckIdleSeconds: 3600, StuckRepetitionCap: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "completed", 5*time.Second)
}

func uniqueObservation(i int) string {
	return "console line #" + string(rune('a'+i))
}

// TestStuckDetectionNoFalsePositiveOnANormalShortRun is the headline
// no-false-positive case: an ordinary short mock run, with stuck detection
// enabled and default-shaped thresholds, must complete normally.
func TestStuckDetectionNoFalsePositiveOnANormalShortRun(t *testing.T) {
	provider := &recordingMockProvider{}
	manager, store, ctx := newStuckHarness(t, provider)
	projectID, agentID := freshProjectAgent(t, store, ctx, "stuck-normal-run")
	run, _, err := manager.Submit(ctx, Job{
		ProjectID: projectID, AgentID: agentID, Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1,
		Prompt:                "Build the first milestone",
		StuckDetectionEnabled: true, StuckIdleSeconds: 600, StuckRepetitionCap: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "completed", 5*time.Second)
}

// TestStuckDetectionDisabledNeverTripsEvenPastEveryThreshold proves
// StuckDetectionEnabled actually gates the whole feature: a provider hung
// well past a 1-second idle limit must be left alone when the job's own flag
// is off (global setting off, the agent opted out, or the operator clicked
// Continue — all fold into this one Job field before Submit).
func TestStuckDetectionDisabledNeverTripsEvenPastEveryThreshold(t *testing.T) {
	provider := &stuckStreamProvider{
		steps: []providers.Event{{Type: "status", RawType: "mock.start", Payload: map[string]any{"message": "started"}}},
		hang:  true,
	}
	manager, store, ctx := newStuckHarness(t, provider)
	manager.tick = 50 * time.Millisecond
	projectID, agentID := freshProjectAgent(t, store, ctx, "stuck-disabled")
	run, _, err := manager.Submit(ctx, Job{
		ProjectID: projectID, AgentID: agentID, Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1,
		Prompt:                "Playtest the obby",
		StuckDetectionEnabled: false, StuckIdleSeconds: 1, StuckRepetitionCap: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "running", 5*time.Second)
	time.Sleep(1500 * time.Millisecond)
	got, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "running" {
		t.Fatalf("run %s status=%s, want it still running with detection disabled", run.ID, got.Status)
	}
	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "cancelled", 5*time.Second)
}

// recordingMockProvider is a minimal deterministic provider for the
// no-false-positive test: a handful of ordinary events, no loop, completing
// promptly, independent of the shared mock.Provider used elsewhere so this
// file has no import cycle back to providers/mock's own test helpers.
type recordingMockProvider struct{}

func (p *recordingMockProvider) Diagnose(context.Context) providers.Diagnostics {
	return providers.Diagnostics{Available: true, Authenticated: true}
}
func (p *recordingMockProvider) Start(_ context.Context, _ providers.RunRequest) (providers.RunHandle, error) {
	steps := []providers.Event{
		{Type: "status", RawType: "mock.start", Payload: map[string]any{"message": "started"}},
		toolUseAssistantEvent("Read"),
		toolResultEvent("file contents"),
		{Type: "message", RawType: "assistant.final", Payload: map[string]any{"text": "Done."}},
	}
	h := &stuckStreamHandle{events: make(chan providers.Event, 16), done: make(chan struct{}), stop: make(chan struct{})}
	go h.stream(&stuckStreamProvider{steps: steps, stepDelay: 2 * time.Millisecond})
	return h, nil
}
func (p *recordingMockProvider) Resume(ctx context.Context, req providers.ResumeRequest) (providers.RunHandle, error) {
	return p.Start(ctx, req.RunRequest)
}
func (p *recordingMockProvider) Cancel(context.Context, string) error { return nil }

// TestCancelWaitingDecisionRunReachesCancelled is the fast, direct unit test
// of Manager.Cancel's new waiting_decision branch: a run parked there has no
// goroutine at all (its own already fully exited, exactly as it does for the
// agent's own natural question), so Cancel must write the
// waiting_decision->cancelling->cancelled sequence directly against the
// store rather than looking for anything in m.active or the queue.
func TestCancelWaitingDecisionRunReachesCancelled(t *testing.T) {
	manager, store, ctx := newStuckHarness(t, &recordingMockProvider{})
	projectID, agentID := freshProjectAgent(t, store, ctx, "stuck-cancel-waiting")
	run, _, err := store.CreateRun(ctx, models.Run{ProjectID: projectID, AgentID: agentID, Provider: "mock", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRunStuck(ctx, run.ID, "waiting_decision", "waiting_decision", "", ""); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Cancel on a waiting_decision run took %v, want it to return almost immediately (no goroutine to signal)", elapsed)
	}
	waitStatus(t, store, run.ID, "cancelled", 2*time.Second)
}

// TestCancelOfARunThatIsNeitherActiveQueuedNorWaitingDecisionStillFails keeps
// the existing "unknown run" behavior intact: Cancel must still reject
// anything that is not active, queued, or waiting_decision (e.g. an already
// completed run), not silently succeed.
func TestCancelOfARunThatIsNeitherActiveQueuedNorWaitingDecisionStillFails(t *testing.T) {
	manager, store, ctx := newStuckHarness(t, &recordingMockProvider{})
	projectID, agentID := freshProjectAgent(t, store, ctx, "stuck-cancel-completed")
	run, _, err := store.CreateRun(ctx, models.Run{ProjectID: projectID, AgentID: agentID, Provider: "mock", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRun(ctx, run.ID, "completed", "verified", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := manager.Cancel(ctx, run.ID); err == nil {
		t.Fatal("Cancel on an already-completed run must still be rejected")
	}
}
