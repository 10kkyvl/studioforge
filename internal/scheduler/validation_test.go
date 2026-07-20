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
	"github.com/10kkyvl/studioforge/internal/providers/mock"
	"github.com/10kkyvl/studioforge/internal/resources"
)

// grantedJob is the shared shape of a Claude, non-plan job with a real Studio
// grant and validation opted in — the one configuration the loop must
// actually run for.
func grantedJob(t *testing.T) Job {
	return Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "claude", Model: "balanced", PermissionProfile: "workspace-write", WorkingDirectory: t.TempDir(), MaxBudget: 1, ValidateAfterRun: true, MaxCorrectionRuns: 1}
}

func withGrant(manager *Manager) {
	manager.SetMCPProvisioner(func(context.Context, *Job) MCPGrant {
		return MCPGrant{ConfigPath: "C:\\configs\\run.json", AllowedTools: []string{"mcp__Roblox_Studio__start_stop_play"}}
	})
}

func waitForRunValidation(t *testing.T, store interface {
	Run(context.Context, string) (models.Run, error)
}, ctx context.Context, runID string, want string) models.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.Run(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Validation == want {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s never reached validation=%q", runID, want)
	return models.Run{}
}

func TestValidationNeverRunsWithoutAValidatorInstalled(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	withGrant(manager)
	run, _, err := manager.Submit(ctx, grantedJob(t))
	if err != nil {
		t.Fatal(err)
	}
	waitForRunValidation(t, store, ctx, run.ID, "none")
}

func TestValidationSkippedWhenAgentOptedOut(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	withGrant(manager)
	called := false
	manager.SetMCPValidator(func(context.Context, *Job) ValidationResult {
		called = true
		return ValidationResult{Outcome: ValidationPassed}
	})
	job := grantedJob(t)
	job.ValidateAfterRun = false
	run, _, err := manager.Submit(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	waitForRunValidation(t, store, ctx, run.ID, "none")
	if called {
		t.Error("an agent that did not opt in must never trigger the validator")
	}
}

func TestValidationSkippedInPlanMode(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	withGrant(manager)
	called := false
	manager.SetMCPValidator(func(context.Context, *Job) ValidationResult {
		called = true
		return ValidationResult{Outcome: ValidationPassed}
	})
	job := grantedJob(t)
	job.Mode = "plan"
	run, _, err := manager.Submit(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	waitForRunValidation(t, store, ctx, run.ID, "none")
	if called {
		t.Error("plan mode must never trigger the validator")
	}
}

func TestValidationSkippedWithoutAStudioGrant(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	manager.SetMCPProvisioner(func(context.Context, *Job) MCPGrant { return MCPGrant{} })
	called := false
	manager.SetMCPValidator(func(context.Context, *Job) ValidationResult {
		called = true
		return ValidationResult{Outcome: ValidationPassed}
	})
	run, _, err := manager.Submit(ctx, grantedJob(t))
	if err != nil {
		t.Fatal(err)
	}
	waitForRunValidation(t, store, ctx, run.ID, "none")
	if called {
		t.Error("no Studio grant must never trigger the validator")
	}
}

func TestValidationSkippedForReadOnlyProfile(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	withGrant(manager)
	called := false
	manager.SetMCPValidator(func(context.Context, *Job) ValidationResult {
		called = true
		return ValidationResult{Outcome: ValidationPassed}
	})
	job := grantedJob(t)
	job.PermissionProfile = "read-only"
	run, _, err := manager.Submit(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	waitForRunValidation(t, store, ctx, run.ID, "none")
	if called {
		t.Error("a read-only run must never trigger the daemon's own Play-mode validator")
	}
}

func TestValidationRunsAndPersistsPassedOutcome(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	withGrant(manager)
	var gotJob *Job
	manager.SetMCPValidator(func(_ context.Context, j *Job) ValidationResult {
		gotJob = j
		return ValidationResult{Outcome: ValidationPassed, Screenshot: "C:\\shots\\1.png"}
	})
	run, _, err := manager.Submit(ctx, grantedJob(t))
	if err != nil {
		t.Fatal(err)
	}
	got := waitForRunValidation(t, store, ctx, run.ID, "passed")
	if got.ValidationScreenshot != "C:\\shots\\1.png" {
		t.Errorf("validationScreenshot=%q", got.ValidationScreenshot)
	}
	if gotJob == nil || gotJob.RunID != run.ID {
		t.Error("validator must receive the job that just completed")
	}
	events, err := store.EventsAfter(ctx, 0, "demo-obby", run.ID, 500)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Type == "validation" {
			found = true
		}
	}
	if !found {
		t.Error("a validation run event must be published so it survives in the transcript")
	}
}

func TestValidationFailureSchedulesACorrectionRun(t *testing.T) {
	manager, provider, store, ctx := newHarness(t)
	withGrant(manager)
	manager.SetMCPValidator(func(_ context.Context, j *Job) ValidationResult {
		if j.ParentRunID != "" {
			// The correction run itself must not recurse into another
			// correction — its own validator call here is a different test.
			return ValidationResult{Outcome: ValidationPassed}
		}
		return ValidationResult{Outcome: ValidationFailed, Errors: []string{"attempt to index nil"}}
	})
	run, _, err := manager.Submit(ctx, grantedJob(t))
	if err != nil {
		t.Fatal(err)
	}
	waitForRunValidation(t, store, ctx, run.ID, "failed")

	deadline := time.Now().Add(5 * time.Second)
	var corrections []models.Run
	for time.Now().Before(deadline) {
		runs, err := store.ListRuns(ctx, "demo-obby", 50)
		if err != nil {
			t.Fatal(err)
		}
		corrections = corrections[:0]
		for _, r := range runs {
			if r.ParentRunID == run.ID {
				corrections = append(corrections, r)
			}
		}
		if len(corrections) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(corrections) != 1 {
		t.Fatalf("corrections scheduled for %s = %d, want 1", run.ID, len(corrections))
	}
	if corrections[0].CorrectionDepth != 1 {
		t.Errorf("correctionDepth=%d, want 1", corrections[0].CorrectionDepth)
	}

	resumed := waitForResume(t, provider)
	if resumed.Prompt == "" {
		t.Error("the correction run's prompt must describe the failure")
	}
}

// One-hop propagation: when a correction run's own validation passes, its
// direct parent is marked "corrected" rather than left at "failed" forever.
func TestValidationPassOnACorrectionRunMarksTheParentCorrected(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	withGrant(manager)
	manager.SetMCPValidator(func(context.Context, *Job) ValidationResult {
		return ValidationResult{Outcome: ValidationPassed}
	})
	parent, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "claude", ModelAlias: "balanced", Status: "completed", Phase: "verified", Validation: "failed"}, "")
	if err != nil {
		t.Fatal(err)
	}
	job := grantedJob(t)
	job.ParentRunID = parent.ID
	job.CorrectionDepth = 1
	if _, _, err := manager.Submit(ctx, job); err != nil {
		t.Fatal(err)
	}
	waitForRunValidation(t, store, ctx, parent.ID, "corrected")
}

// One-hop propagation on exhaustion: the correction itself failed again and
// no further corrections are allowed, so the parent is marked
// correction_failed rather than staying at a bare "failed" forever.
func TestValidationFailureExhaustedMarksTheParentCorrectionFailed(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	withGrant(manager)
	manager.SetMCPValidator(func(context.Context, *Job) ValidationResult {
		return ValidationResult{Outcome: ValidationFailed, Errors: []string{"still broken"}}
	})
	parent, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "claude", ModelAlias: "balanced", Status: "completed", Phase: "verified", Validation: "failed"}, "")
	if err != nil {
		t.Fatal(err)
	}
	job := grantedJob(t)
	job.ParentRunID = parent.ID
	job.CorrectionDepth = 1 // already at MaxCorrectionRuns (1): no further correction
	job.MaxCorrectionRuns = 1
	if _, _, err := manager.Submit(ctx, job); err != nil {
		t.Fatal(err)
	}
	waitForRunValidation(t, store, ctx, parent.ID, "correction_failed")
}

// Validation must not stall behind the project's normal 5s heartbeat loop,
// which stops draining once the provider process exits — a long validation
// pass has to keep renewing the project write lease itself, or a second run
// for the same project could steal it mid-validation. This is an
// outcome-level test (does the invariant actually hold), not a test of the
// heartbeat mechanism itself.
func TestValidationHeartbeatsTheLeaseWhileRunning(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	withGrant(manager)
	validatorStarted := make(chan struct{})
	var startedOnce sync.Once
	release := make(chan struct{})
	manager.SetMCPValidator(func(context.Context, *Job) ValidationResult {
		startedOnce.Do(func() { close(validatorStarted) })
		<-release
		return ValidationResult{Outcome: ValidationPassed}
	})
	runA, _, err := manager.Submit(ctx, grantedJob(t))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-validatorStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("validator A never started")
	}

	runB, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	// newHarness's lease manager uses a 1-second TTL; without renewal during
	// validation, A's lease would be reaped well within this window, letting
	// B acquire it and start immediately.
	time.Sleep(2 * time.Second)
	b, err := store.Run(ctx, runB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status == "running" || b.Status == "completed" {
		t.Fatalf("run B (status=%q) started while A's validation still held the project write lease", b.Status)
	}

	close(release)
	waitForRunValidation(t, store, ctx, runA.ID, "passed")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err = store.Run(ctx, runB.ID)
		if err != nil {
			t.Fatal(err)
		}
		if b.Status == "completed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if b.Status != "completed" {
		t.Fatalf("run B never completed after A released its lease, status=%q", b.Status)
	}
}

// budgetGateStore wraps a real *database.Store and rejects BudgetAllowed
// starting from its Nth call, so a test can make an original run succeed its
// own budget check while a specific later call (the correction run's) fails
// its own — without needing to reverse-engineer the demo seed's exact
// used-cost figure.
type budgetGateStore struct {
	*database.Store
	mu         sync.Mutex
	calls      int
	rejectFrom int
}

func (s *budgetGateStore) BudgetAllowed(ctx context.Context, projectID string, additional float64) (bool, float64, float64, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if s.rejectFrom > 0 && n >= s.rejectFrom {
		return false, 25, 25, nil
	}
	return s.Store.BudgetAllowed(ctx, projectID, additional)
}

// The budget ceiling must bound the whole correction loop, not just the
// original run: when a correction would exceed it, the loop stops and
// surfaces a failure instead of silently retrying forever.
func TestCorrectionRunExceedingBudgetSurfacesAsAFailureNotASilentRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "budget.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.NewStore(db).SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	store := &budgetGateStore{Store: database.NewStore(db), rejectFrom: 2}
	hub := events.NewHub(store)
	t.Cleanup(hub.Close)
	leases := resources.NewManager(time.Second)
	t.Cleanup(leases.Close)
	inner := mock.New()
	inner.StepDelay = time.Millisecond
	provider := &recordingProvider{inner: inner}
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider, "claude": provider})
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	withGrant(manager)
	manager.SetMCPValidator(func(context.Context, *Job) ValidationResult {
		return ValidationResult{Outcome: ValidationFailed, Errors: []string{"boom"}}
	})

	run, _, err := manager.Submit(ctx, grantedJob(t))
	if err != nil {
		t.Fatal(err)
	}
	waitForRunValidation(t, store, ctx, run.ID, "failed")

	deadline := time.Now().Add(5 * time.Second)
	var correction models.Run
	for time.Now().Before(deadline) {
		runs, err := store.ListRuns(ctx, "demo-obby", 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range runs {
			if r.ParentRunID == run.ID {
				correction = r
			}
		}
		if correction.ID != "" {
			got, err := store.Run(ctx, correction.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status == "failed" {
				correction = got
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if correction.ID == "" {
		t.Fatal("no correction run was ever scheduled")
	}
	if correction.Status != "failed" {
		t.Fatalf("correction run status=%q, want failed (budget ceiling reached)", correction.Status)
	}
}
