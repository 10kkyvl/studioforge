package app

import (
	"context"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/mock"
	"github.com/10kkyvl/studioforge/internal/resources"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

func seedPendingReview(t *testing.T, store *database.Store, ctx context.Context) (projectID, runID string) {
	t.Helper()
	project, err := store.CreateProject(ctx, models.Project{Name: "review-project", Path: t.TempDir(), Fingerprint: "review-project"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.CreateAgent(ctx, models.Agent{ProjectID: project.ID, Provider: "mock"})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, models.Run{ID: "recover-run", ProjectID: project.ID, AgentID: agent.ID, Provider: "mock", Status: "waiting_decision", Phase: "waiting_decision"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateReview(ctx, models.RunReview{RunID: run.ID, ProjectID: project.ID, CheckpointHash: "abc123", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return project.ID, run.ID
}

func TestPendingReviewsReacquireTheirLeaseOnStartup(t *testing.T) {
	store, ctx := newStartupSettingsStore(t)
	projectID, runID := seedPendingReview(t, store, ctx)

	leases := resources.NewManager(time.Second)
	defer leases.Close()
	hub := events.NewHub(store)
	defer hub.Close()
	sched := scheduler.New(ctx, store, hub, leases, map[string]providers.Provider{"mock": mock.New()})
	defer func() { _ = sched.Close(context.Background()) }()

	recoverPendingReviews(ctx, store, leases, sched)

	owners := leases.Snapshot()
	if got, want := owners["project:"+projectID+":write"], "review:"+runID; got != want {
		t.Fatalf("owners[project write lease]=%q, want %q", got, want)
	}

	acquireCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := leases.Acquire(acquireCtx, "someone-else", []string{"project:" + projectID + ":write"}); err == nil {
		t.Fatal("the project lease was reacquirable by someone else even though the review is still pending")
	}
}

func TestExpiredReviewsAreMarkedExpiredAndTheirLeaseIsNotReacquired(t *testing.T) {
	store, ctx := newStartupSettingsStore(t)
	project, err := store.CreateProject(ctx, models.Project{Name: "expired-project", Path: t.TempDir(), Fingerprint: "expired-project"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.CreateAgent(ctx, models.Agent{ProjectID: project.ID, Provider: "mock"})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, models.Run{ID: "expired-run", ProjectID: project.ID, AgentID: agent.ID, Provider: "mock", Status: "waiting_decision", Phase: "waiting_decision"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateReview(ctx, models.RunReview{RunID: run.ID, ProjectID: project.ID, CheckpointHash: "abc123", ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	leases := resources.NewManager(time.Second)
	defer leases.Close()
	hub := events.NewHub(store)
	defer hub.Close()
	sched := scheduler.New(ctx, store, hub, leases, map[string]providers.Provider{"mock": mock.New()})
	defer func() { _ = sched.Close(context.Background()) }()

	recoverPendingReviews(ctx, store, leases, sched)

	if owners := leases.Snapshot(); len(owners) != 0 {
		t.Fatalf("owners=%+v, want no lease held for an already-expired review", owners)
	}
	review, ok, err := store.Review(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || review.Status != "expired" {
		t.Fatalf("review=%+v ok=%v, want status=expired", review, ok)
	}
}
