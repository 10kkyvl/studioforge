package scheduler

import (
	"context"
	"errors"
	"os"
	"os/exec"
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

type checkpointGateStore struct {
	*database.Store
	mu             sync.Mutex
	creates        int
	failCheckpoint bool
	failCreateRun  bool
}

func (s *checkpointGateStore) CreateCheckpoint(ctx context.Context, checkpoint models.Checkpoint) error {
	s.mu.Lock()
	fail := s.failCheckpoint
	s.mu.Unlock()
	if fail {
		return errors.New("checkpoint persist failed")
	}
	if err := s.Store.CreateCheckpoint(ctx, checkpoint); err != nil {
		return err
	}
	s.mu.Lock()
	s.creates++
	s.mu.Unlock()
	return nil
}

func (s *checkpointGateStore) CreateRun(ctx context.Context, run models.Run, idempotencyKey string) (models.Run, bool, error) {
	s.mu.Lock()
	fail := s.failCreateRun
	s.mu.Unlock()
	if fail {
		return models.Run{}, false, errors.New("create run failed")
	}
	return s.Store.CreateRun(ctx, run, idempotencyKey)
}

func (s *checkpointGateStore) createCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creates
}

func gitRepoWithChanges(t *testing.T) string {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@t.local")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newCheckpointHarness(t *testing.T) (*Manager, *checkpointGateStore, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "checkpoint.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.NewStore(db).SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	store := &checkpointGateStore{Store: database.NewStore(db)}
	hub := events.NewHub(store)
	t.Cleanup(hub.Close)
	leases := resources.NewManager(time.Second)
	t.Cleanup(leases.Close)
	inner := mock.New()
	inner.StepDelay = time.Millisecond
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": inner, "claude": inner})
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return manager, store, ctx
}

func newCorrectionParent(t *testing.T, ctx context.Context, store *checkpointGateStore, dir string) (models.Run, *Job) {
	t.Helper()
	parent, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced", Status: "completed", Phase: "verified"}, "")
	if err != nil {
		t.Fatal(err)
	}
	parentJob := &Job{RunID: parent.ID, ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: dir, MaxCorrectionRuns: 3}
	return parent, parentJob
}

func correctionRunsFor(t *testing.T, ctx context.Context, store *checkpointGateStore, parentID string) []models.Run {
	t.Helper()
	runs, err := store.ListRuns(ctx, "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []models.Run
	for _, r := range runs {
		if r.ParentRunID == parentID {
			out = append(out, r)
		}
	}
	return out
}

func waitForRunStatus(t *testing.T, ctx context.Context, store *checkpointGateStore, runID, want string) models.Run {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.Run(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == want {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s never reached status=%q", runID, want)
	return models.Run{}
}

func failedValidation() ValidationResult {
	return ValidationResult{Outcome: ValidationFailed, Errors: []string{"boom"}}
}

func TestCorrectionProviderNotConfiguredCreatesNoCheckpoint(t *testing.T) {
	manager, store, ctx := newCheckpointHarness(t)
	dir := gitRepoWithChanges(t)
	parent, parentJob := newCorrectionParent(t, ctx, store, dir)
	parentJob.Provider = "ghost"

	manager.scheduleCorrection(ctx, parentJob, "sess-1", failedValidation())

	if got := store.createCount(); got != 0 {
		t.Errorf("createCount=%d, want 0", got)
	}
	if corrections := correctionRunsFor(t, ctx, store, parent.ID); len(corrections) != 0 {
		t.Errorf("corrections=%d, want 0", len(corrections))
	}
}

func TestCorrectionSchedulerClosedCreatesNoCheckpoint(t *testing.T) {
	manager, store, ctx := newCheckpointHarness(t)
	dir := gitRepoWithChanges(t)
	parent, parentJob := newCorrectionParent(t, ctx, store, dir)

	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager.scheduleCorrection(ctx, parentJob, "sess-1", failedValidation())

	if got := store.createCount(); got != 0 {
		t.Errorf("createCount=%d, want 0", got)
	}
	if corrections := correctionRunsFor(t, ctx, store, parent.ID); len(corrections) != 0 {
		t.Errorf("corrections=%d, want 0", len(corrections))
	}
}

func TestCorrectionCreateRunFailureCreatesNoCheckpoint(t *testing.T) {
	manager, store, ctx := newCheckpointHarness(t)
	dir := gitRepoWithChanges(t)
	_, parentJob := newCorrectionParent(t, ctx, store, dir)
	store.failCreateRun = true

	manager.scheduleCorrection(ctx, parentJob, "sess-1", failedValidation())

	if got := store.createCount(); got != 0 {
		t.Errorf("createCount=%d, want 0", got)
	}
}

func TestCorrectionCheckpointPersistFailureAbortsWithoutRunning(t *testing.T) {
	manager, store, ctx := newCheckpointHarness(t)
	dir := gitRepoWithChanges(t)
	parent, parentJob := newCorrectionParent(t, ctx, store, dir)
	store.failCheckpoint = true

	manager.scheduleCorrection(ctx, parentJob, "sess-1", failedValidation())

	if got := store.createCount(); got != 0 {
		t.Errorf("createCount=%d, want 0", got)
	}
	corrections := correctionRunsFor(t, ctx, store, parent.ID)
	if len(corrections) != 1 {
		t.Fatalf("corrections=%d, want 1", len(corrections))
	}
	run := waitForRunStatus(t, ctx, store, corrections[0].ID, "failed")
	if run.Status == "running" || run.Status == "queued" || run.Status == "completed" {
		t.Errorf("status=%q, must not be running/queued/completed", run.Status)
	}
}

func TestDuplicateCorrectionSchedulingCreatesOneCheckpoint(t *testing.T) {
	manager, store, ctx := newCheckpointHarness(t)
	dir := gitRepoWithChanges(t)
	parent, parentJob := newCorrectionParent(t, ctx, store, dir)

	manager.scheduleCorrection(ctx, parentJob, "sess-1", failedValidation())
	manager.scheduleCorrection(ctx, parentJob, "sess-1", failedValidation())

	if got := store.createCount(); got != 1 {
		t.Errorf("createCount=%d, want 1", got)
	}
	if corrections := correctionRunsFor(t, ctx, store, parent.ID); len(corrections) != 1 {
		t.Errorf("corrections=%d, want 1", len(corrections))
	}
}

func TestSuccessfulCorrectionRecordsOneCheckpointLinkedToItsRun(t *testing.T) {
	manager, store, ctx := newCheckpointHarness(t)
	dir := gitRepoWithChanges(t)
	parent, parentJob := newCorrectionParent(t, ctx, store, dir)

	manager.scheduleCorrection(ctx, parentJob, "sess-1", failedValidation())

	corrections := correctionRunsFor(t, ctx, store, parent.ID)
	if len(corrections) != 1 {
		t.Fatalf("corrections=%d, want 1", len(corrections))
	}
	checkpoint, err := store.CheckpointForRun(ctx, corrections[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.CommitHash == "" {
		t.Error("checkpoint commit hash must not be empty")
	}
	if checkpoint.RunID != corrections[0].ID {
		t.Errorf("checkpoint runID=%q, want %q", checkpoint.RunID, corrections[0].ID)
	}
	if got := store.createCount(); got != 1 {
		t.Errorf("createCount=%d, want 1", got)
	}
}
