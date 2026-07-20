package scheduler

import (
	"context"
	"errors"
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

func fastMock() *mock.Provider {
	p := mock.New()
	p.StepDelay = time.Millisecond
	return p
}

func slowMock() *mock.Provider {
	p := mock.New()
	p.StepDelay = 200 * time.Millisecond
	return p
}

type failingUpdateStore struct {
	*database.Store
	mu   sync.Mutex
	fail map[string]bool
}

func (s *failingUpdateStore) UpdateRun(ctx context.Context, id, status, phase, resource, errText string) error {
	s.mu.Lock()
	blocked := s.fail[status]
	s.mu.Unlock()
	if blocked {
		return errors.New("injected storage failure writing status " + status)
	}
	return s.Store.UpdateRun(ctx, id, status, phase, resource, errText)
}

func newFailingHarness(t *testing.T, fail map[string]bool, provider providers.Provider) (*Manager, *failingUpdateStore, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "fail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base := database.NewStore(db)
	if err := base.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	store := &failingUpdateStore{Store: base, fail: fail}
	hub := events.NewHub(store)
	t.Cleanup(hub.Close)
	leases := resources.NewManager(time.Second)
	t.Cleanup(leases.Close)
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider, "claude": provider})
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return manager, store, ctx
}

func waitStorageError(t *testing.T, store *failingUpdateStore, ctx context.Context, runID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list, err := store.EventsAfter(ctx, 0, "", runID, 500)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			if e.RawType == "scheduler.storage_error" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no scheduler.storage_error event was published for run %s", runID)
}

func hasTerminalStatusEvent(t *testing.T, store *failingUpdateStore, ctx context.Context, runID, status string) bool {
	t.Helper()
	list, err := store.EventsAfter(ctx, 0, "", runID, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.Type != "status" {
			continue
		}
		payload, ok := e.Payload.(map[string]any)
		if ok && payload["status"] == status {
			return true
		}
	}
	return false
}

func TestFailedCompletedWriteEmitsStorageErrorNotFalseCompleted(t *testing.T) {
	manager, store, ctx := newFailingHarness(t, map[string]bool{"completed": true}, fastMock())
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStorageError(t, store, ctx, run.ID)
	if hasTerminalStatusEvent(t, store, ctx, run.ID, "completed") {
		t.Error("a completed status event must never be published when the completed write failed")
	}
	got, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == "completed" {
		t.Errorf("DB status=%q, must not be completed after its write failed", got.Status)
	}
	if _, err := store.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "interrupted" {
		t.Errorf("after RecoverInterrupted, status=%q, want interrupted so a failed terminal write is recoverable", got.Status)
	}
}

func TestFailedCancelledWriteEmitsStorageErrorNotFalseCancelled(t *testing.T) {
	manager, store, ctx := newFailingHarness(t, map[string]bool{"cancelled": true}, slowMock())
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store.Store, run.ID, "running", 5*time.Second)
	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStorageError(t, store, ctx, run.ID)
	if hasTerminalStatusEvent(t, store, ctx, run.ID, "cancelled") {
		t.Error("a cancelled status event must never be published when the cancelled write failed")
	}
	got, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == "cancelled" {
		t.Errorf("DB status=%q, must not be cancelled after its write failed", got.Status)
	}
}

func TestFailedFailWriteEmitsStorageErrorNotFalseFailed(t *testing.T) {
	manager, store, ctx := newFailingHarness(t, map[string]bool{"failed": true}, fastMock())
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", Scenario: "crash", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStorageError(t, store, ctx, run.ID)
	if hasTerminalStatusEvent(t, store, ctx, run.ID, "failed") {
		t.Error("a failed status event must never be published when the failed write failed")
	}
	list, err := store.EventsAfter(ctx, 0, "", run.ID, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.RawType == "scheduler.failed" {
			t.Error("a scheduler.failed error event must not be published when the failed write failed")
		}
	}
	got, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == "failed" {
		t.Errorf("DB status=%q, must not be failed after its write failed", got.Status)
	}
}

func TestPauseRecordsResumablePausedStateWithSavedSession(t *testing.T) {
	manager, provider, store, ctx := newHarness(t)
	if inner, ok := provider.inner.(*mock.Provider); ok {
		inner.StepDelay = 200 * time.Millisecond
	}
	thread, err := store.EnsureDefaultThread(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", ThreadID: thread.ID, WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "running", 5*time.Second)
	if err := manager.Pause(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "paused", 3*time.Second)
	got, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderSession == "" {
		t.Error("a paused run must save its provider session so it can be resumed")
	}
	session, err := store.LatestThreadSession(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session != got.ProviderSession {
		t.Errorf("LatestThreadSession=%q, want the paused run's saved session %q so the next message resumes it", session, got.ProviderSession)
	}
}

func TestPauseThenCancelEndsCancelled(t *testing.T) {
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
	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "cancelled", 5*time.Second)
}

func TestPauseRacingCompletionSettlesToOneStableTerminalState(t *testing.T) {
	manager, provider, store, ctx := newHarness(t)
	if inner, ok := provider.inner.(*mock.Provider); ok {
		inner.StepDelay = 5 * time.Millisecond
	}
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	hammered := make(chan struct{})
	go func() {
		defer close(hammered)
		for i := 0; i < 60; i++ {
			_ = manager.Pause(ctx, run.ID)
			time.Sleep(time.Millisecond)
		}
	}()
	<-hammered
	terminal := map[string]bool{"completed": true, "paused": true, "cancelled": true}
	deadline := time.Now().Add(5 * time.Second)
	var settled string
	for time.Now().Before(deadline) {
		got, err := store.Run(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if terminal[got.Status] {
			settled = got.Status
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if settled == "" {
		t.Fatal("run never settled: pausing around completion must not deadlock or leave it non-terminal")
	}
	time.Sleep(150 * time.Millisecond)
	got, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != settled {
		t.Errorf("run status changed from %q to %q after settling: the terminal state must be written exactly once", settled, got.Status)
	}
}

func TestCancelQueuedRunReachesCancelledWithoutStarting(t *testing.T) {
	manager, provider, store, ctx := newHarness(t)
	manager.SetLimits(1, 1, 1, 1)
	occupier, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", Scenario: "hang", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, occupier.ID, "running", 5*time.Second)
	queued, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Run(ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "queued" {
		t.Fatalf("second run status=%q, want queued behind the single running slot", got.Status)
	}
	if err := manager.Cancel(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, queued.ID, "cancelled", 3*time.Second)
	for _, req := range provider.requests() {
		if req.RunID == queued.ID {
			t.Error("a run cancelled while queued must never be sent to the provider")
		}
	}
	_ = manager.Cancel(ctx, occupier.ID)
}

func TestValidationLeaseLossAbortsAsInconclusiveWithoutCorrection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "leaseloss.db"))
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
	leases := resources.NewManager(400 * time.Millisecond)
	t.Cleanup(leases.Close)
	provider := fastMock()
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider, "claude": provider})
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	withGrant(manager)
	validatorStarted := make(chan struct{})
	manager.SetMCPValidator(func(vctx context.Context, _ *Job) ValidationResult {
		close(validatorStarted)
		<-vctx.Done()
		return ValidationResult{Outcome: ValidationPassed}
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
	got := waitForRunValidation(t, store, ctx, run.ID, "inconclusive")
	if got.Validation != "inconclusive" {
		t.Fatalf("validation=%q, want inconclusive after the lease was lost, never passed", got.Validation)
	}
	runs, err := store.ListRuns(ctx, "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if r.ParentRunID == run.ID {
			t.Errorf("a correction run %s was scheduled after lease loss; none should be", r.ID)
		}
	}
	for key, owner := range leases.Snapshot() {
		if owner == run.ID {
			t.Errorf("lease %s is still owned by run %s after its lease was lost", key, run.ID)
		}
	}
}
