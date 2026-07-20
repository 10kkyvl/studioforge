package scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/mock"
	"github.com/10kkyvl/studioforge/internal/resources"
)

type slowCancelProvider struct {
	cancelDelay    time.Duration
	streamInterval time.Duration
	// maxEvents, when positive, stops the handle emitting after that many
	// events without terminating it — a stream that goes silent mid-run, for
	// the idle-check tests.
	maxEvents int
}

func (p *slowCancelProvider) Diagnose(context.Context) providers.Diagnostics {
	return providers.Diagnostics{Available: true, Authenticated: true}
}
func (p *slowCancelProvider) Start(_ context.Context, _ providers.RunRequest) (providers.RunHandle, error) {
	interval := p.streamInterval
	if interval <= 0 {
		interval = time.Millisecond
	}
	h := &slowCancelHandle{events: make(chan providers.Event, 32), done: make(chan struct{}), stop: make(chan struct{}), cancelDelay: p.cancelDelay, maxEvents: p.maxEvents}
	go h.stream(interval)
	return h, nil
}
func (p *slowCancelProvider) Resume(ctx context.Context, req providers.ResumeRequest) (providers.RunHandle, error) {
	return p.Start(ctx, req.RunRequest)
}
func (p *slowCancelProvider) Cancel(context.Context, string) error { return nil }

type slowCancelHandle struct {
	events      chan providers.Event
	done        chan struct{}
	stop        chan struct{}
	once        sync.Once
	cancelDelay time.Duration
	maxEvents   int
	result      providers.Result
}

func (h *slowCancelHandle) stream(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	sent := 0
	for {
		select {
		case <-h.stop:
			h.result = providers.Result{SessionID: "sess-slow-cancel", ExitCode: -1}
			close(h.events)
			close(h.done)
			return
		case <-ticker.C:
			if h.maxEvents > 0 && sent >= h.maxEvents {
				continue
			}
			select {
			case h.events <- providers.Event{Type: "message", RawType: "assistant.partial", Payload: map[string]any{"text": "streaming"}, At: time.Now().UTC()}:
				sent++
			case <-h.stop:
				h.result = providers.Result{SessionID: "sess-slow-cancel", ExitCode: -1}
				close(h.events)
				close(h.done)
				return
			}
		}
	}
}
func (h *slowCancelHandle) Events() <-chan providers.Event { return h.events }
func (h *slowCancelHandle) Wait() providers.Result         { <-h.done; return h.result }
func (h *slowCancelHandle) Cancel() error {
	h.once.Do(func() {
		time.Sleep(h.cancelDelay)
		close(h.stop)
	})
	return nil
}

func TestCancelReturnsPromptlyDespiteSlowTermination(t *testing.T) {
	provider := &slowCancelProvider{cancelDelay: 500 * time.Millisecond, streamInterval: 5 * time.Millisecond}
	manager, store, ctx := newUsageHarness(t, provider)
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "running", 5*time.Second)

	start := time.Now()
	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Manager.Cancel took %v despite a 500ms termination path, want it to return almost immediately", elapsed)
	}
	waitStatus(t, store, run.ID, "cancelled", 5*time.Second)
}

func TestCancelDuringActiveStreamingDoesNotDeadlock(t *testing.T) {
	provider := &slowCancelProvider{streamInterval: 200 * time.Microsecond}
	manager, store, ctx := newUsageHarness(t, provider)
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "running", 5*time.Second)
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Manager.Cancel took %v during active event streaming, want almost immediate", elapsed)
	}
	waitStatus(t, store, run.ID, "cancelled", 5*time.Second)
}

// TestCancelDuringStuckEscalationReachesCancelledNotWaitingDecision covers
// the race between Manager.Cancel and escalateStuck's own slow
// handle.Cancel()/handle.Wait(): the run stays in m.active for all of that
// call (it is only removed once run() itself returns), so a Cancel landing
// while escalateStuck is still mid-flight must still win out and land the run
// on cancelled, not have its stop silently dropped in favor of
// escalateStuck's unconditional waiting_decision write.
func TestCancelDuringStuckEscalationReachesCancelledNotWaitingDecision(t *testing.T) {
	provider := &slowCancelProvider{cancelDelay: 500 * time.Millisecond, streamInterval: 2 * time.Millisecond, maxEvents: 5}
	manager, store, ctx := newUsageHarness(t, provider)
	manager.tick = 50 * time.Millisecond
	run, _, err := manager.Submit(ctx, Job{
		ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1,
		Prompt:                "Playtest the obby",
		StuckDetectionEnabled: true, StuckIdleSeconds: 1, StuckRepetitionCap: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "running", 5*time.Second)
	// The stream goes silent after 5 events (~10ms in), so the 1-second idle
	// limit trips at the first tick past the one-second mark. This sleep lands
	// after escalateStuck has already called handle.Cancel() and is blocked in
	// handle.Wait() for the full 500ms cancelDelay — exactly the window Cancel
	// below must race into.
	time.Sleep(1200 * time.Millisecond)

	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "cancelled", 5*time.Second)

	got, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "cancelled" {
		t.Fatalf("run status=%q, want cancelled: a Cancel that raced escalateStuck's slow handle.Wait() must not be silently dropped in favor of waiting_decision", got.Status)
	}
	if got.StuckEscalated {
		t.Errorf("run stuckEscalated=true, want false: a run cancelled mid-escalation must not be reported as having escalated")
	}
}

func TestCancelWhilePausedMidStreamReachesCancelledWithoutDeadlock(t *testing.T) {
	manager, provider, store, ctx := newHarness(t)
	if inner, ok := provider.inner.(*mock.Provider); ok {
		inner.StepDelay = 200 * time.Millisecond
	}
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "running", 5*time.Second)
	if err := manager.Pause(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "paused", 2*time.Second)
	time.Sleep(400 * time.Millisecond)

	start := time.Now()
	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Manager.Cancel took %v while paused mid-stream, want almost immediate", elapsed)
	}
	waitStatus(t, store, run.ID, "cancelled", 5*time.Second)
}

func TestCancelDuringRunValidationReachesCancelledNotStuckAtRunning(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	withGrant(manager)
	validatorStarted := make(chan struct{})
	manager.SetMCPValidator(func(ctx context.Context, _ *Job) ValidationResult {
		close(validatorStarted)
		<-ctx.Done()
		return ValidationResult{Outcome: ValidationInconclusive}
	})
	run, _, err := manager.Submit(ctx, grantedJob(t))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-validatorStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("validator never started")
	}
	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "cancelled", 5*time.Second)
}

func TestPauseAfterCancelReturnsErrorAndRunStillReachesCancelled(t *testing.T) {
	provider := &slowCancelProvider{cancelDelay: 300 * time.Millisecond, streamInterval: 5 * time.Millisecond}
	manager, store, ctx := newUsageHarness(t, provider)
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "running", 5*time.Second)

	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Pause(ctx, run.ID); err == nil {
		t.Fatal("Pause after Cancel must return an error, not race the cancelling run")
	}
	waitStatus(t, store, run.ID, "cancelled", 5*time.Second)
}

func TestResumeAfterCancelReturnsErrorAndRunStillReachesCancelled(t *testing.T) {
	manager, provider, store, ctx := newHarness(t)
	if inner, ok := provider.inner.(*mock.Provider); ok {
		inner.StepDelay = 200 * time.Millisecond
	}
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "running", 5*time.Second)
	if err := manager.Pause(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "paused", 2*time.Second)

	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Resume(ctx, run.ID); err == nil {
		t.Fatal("Resume after Cancel must return an error, not race the cancelling run")
	}
	waitStatus(t, store, run.ID, "cancelled", 5*time.Second)
}

type delayedUpdateStore struct {
	*database.Store
	hold    chan struct{}
	entered chan struct{}
}

func (s *delayedUpdateStore) UpdateRunIfStatus(ctx context.Context, id string, expectedStatuses []string, status, phase, resource, errText string) (bool, error) {
	if s.entered != nil {
		s.entered <- struct{}{}
	}
	if s.hold != nil {
		<-s.hold
	}
	return s.Store.UpdateRunIfStatus(ctx, id, expectedStatuses, status, phase, resource, errText)
}

func newDelayedUpdateHarness(t *testing.T, provider providers.Provider) (*Manager, *delayedUpdateStore, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "cas.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.NewStore(db).SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	store := &delayedUpdateStore{Store: database.NewStore(db)}
	hub := events.NewHub(store)
	t.Cleanup(hub.Close)
	leases := resources.NewManager(time.Second)
	t.Cleanup(leases.Close)
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider})
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return manager, store, ctx
}

func TestPauseCASRejectedByConcurrentCancelLeavesTerminalStatusIntact(t *testing.T) {
	provider := &slowCancelProvider{streamInterval: 2 * time.Millisecond}
	manager, store, ctx := newDelayedUpdateHarness(t, provider)
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store.Store, run.ID, "running", 5*time.Second)

	store.entered = make(chan struct{}, 1)
	store.hold = make(chan struct{})

	pauseErr := make(chan error, 1)
	go func() { pauseErr <- manager.Pause(ctx, run.ID) }()

	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Pause never reached its guarded store write")
	}

	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store.Store, run.ID, "cancelled", 5*time.Second)

	close(store.hold)
	if err := <-pauseErr; err == nil {
		t.Fatal("Pause must return an error when its guarded write is rejected by a run that already reached a terminal status")
	}

	got, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "cancelled" {
		t.Fatalf("run status=%q, want the concurrently-cancelled status to survive the delayed Pause write", got.Status)
	}
}

func TestResumeCASRejectedByConcurrentCancelLeavesTerminalStatusIntact(t *testing.T) {
	inner := mock.New()
	inner.StepDelay = 200 * time.Millisecond
	manager, store, ctx := newDelayedUpdateHarness(t, inner)
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store.Store, run.ID, "running", 5*time.Second)
	if err := manager.Pause(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store.Store, run.ID, "paused", 2*time.Second)

	store.entered = make(chan struct{}, 1)
	store.hold = make(chan struct{})

	resumeErr := make(chan error, 1)
	go func() { resumeErr <- manager.Resume(ctx, run.ID) }()

	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Resume never reached its guarded store write")
	}

	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store.Store, run.ID, "cancelled", 5*time.Second)

	close(store.hold)
	if err := <-resumeErr; err == nil {
		t.Fatal("Resume must return an error when its guarded write is rejected by a run that already reached a terminal status")
	}

	got, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "cancelled" {
		t.Fatalf("run status=%q, want the concurrently-cancelled status to survive the delayed Resume write", got.Status)
	}
}
