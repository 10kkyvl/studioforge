package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

func seedReviewFixture(t *testing.T, store *Store) (models.Project, models.Run) {
	t.Helper()
	ctx := context.Background()
	project, err := store.CreateProject(ctx, models.Project{Name: "Review project", Path: filepath.Join(t.TempDir(), "review-project"), Fingerprint: "review-project"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.CreateAgent(ctx, models.Agent{ProjectID: project.ID, Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, models.Run{ProjectID: project.ID, AgentID: agent.ID, Provider: "claude", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	return project, run
}

func TestCreateReviewAndReadItBack(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, run := seedReviewFixture(t, store)
	now := time.Now().UTC()

	created, err := store.CreateReview(ctx, models.RunReview{ProjectID: project.ID, RunID: run.ID, CheckpointHash: "abc123", ExpiresAt: now.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Status != "pending" {
		t.Fatalf("created=%+v, want a pending id assigned", created)
	}

	found, ok, err := store.Review(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("review not found by run id")
	}
	if found.ID != created.ID || found.CheckpointHash != "abc123" || found.ProjectID != project.ID {
		t.Fatalf("found=%+v, want it to match the created review", found)
	}
}

func TestOnlyOneReviewPerRun(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, run := seedReviewFixture(t, store)
	now := time.Now().UTC()

	if _, err := store.CreateReview(ctx, models.RunReview{ProjectID: project.ID, RunID: run.ID, CheckpointHash: "first", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateReview(ctx, models.RunReview{ProjectID: project.ID, RunID: run.ID, CheckpointHash: "second", ExpiresAt: now.Add(time.Hour)}); err == nil {
		t.Fatal("creating a second review for the same run must fail the unique index")
	}
}

func TestResolveReviewRefusesAnAlreadyResolvedReview(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, run := seedReviewFixture(t, store)
	now := time.Now().UTC()
	review, err := store.CreateReview(ctx, models.RunReview{ProjectID: project.ID, RunID: run.ID, CheckpointHash: "abc123", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveReview(ctx, review.ID, "applied", "", now); err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveReview(ctx, review.ID, "applied", "", now); err == nil {
		t.Fatal("resolving an already-resolved review must error, not silently succeed twice")
	}
}

func TestExpireReviewsTakesOnlyPastDuePendingRows(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, run := seedReviewFixture(t, store)
	_, otherRun := seedReviewFixture(t, store)
	now := time.Now().UTC()

	pastDue, err := store.CreateReview(ctx, models.RunReview{ProjectID: project.ID, RunID: run.ID, CheckpointHash: "past-due", ExpiresAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	future, err := store.CreateReview(ctx, models.RunReview{ProjectID: project.ID, RunID: otherRun.ID, CheckpointHash: "future", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	_, alreadyResolvedRun := seedReviewFixture(t, store)
	alreadyResolved, err := store.CreateReview(ctx, models.RunReview{ProjectID: project.ID, RunID: alreadyResolvedRun.ID, CheckpointHash: "resolved", ExpiresAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveReview(ctx, alreadyResolved.ID, "applied", "", now); err != nil {
		t.Fatal(err)
	}

	expired, err := store.ExpireReviews(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0].ID != pastDue.ID {
		t.Fatalf("expired=%+v, want only the past-due pending review", expired)
	}

	stillFuture, ok, err := store.Review(ctx, future.RunID)
	if err != nil || !ok || stillFuture.Status != "pending" {
		t.Fatalf("future review=%+v ok=%v err=%v, want it left pending", stillFuture, ok, err)
	}
	stillResolved, ok, err := store.Review(ctx, alreadyResolvedRun.ID)
	if err != nil || !ok || stillResolved.Status != "applied" {
		t.Fatalf("already-resolved review=%+v ok=%v err=%v, want it left applied", stillResolved, ok, err)
	}
	nowExpired, ok, err := store.Review(ctx, pastDue.RunID)
	if err != nil || !ok || nowExpired.Status != "expired" || nowExpired.ResolvedAt == nil {
		t.Fatalf("past-due review=%+v ok=%v err=%v, want it marked expired with resolvedAt set", nowExpired, ok, err)
	}
}

func TestPendingReviewsListsOnlyPending(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, run := seedReviewFixture(t, store)
	_, otherRun := seedReviewFixture(t, store)
	now := time.Now().UTC()

	pending, err := store.CreateReview(ctx, models.RunReview{ProjectID: project.ID, RunID: run.ID, CheckpointHash: "pending", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := store.CreateReview(ctx, models.RunReview{ProjectID: project.ID, RunID: otherRun.ID, CheckpointHash: "resolved", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveReview(ctx, resolved.ID, "rejected", "", now); err != nil {
		t.Fatal(err)
	}

	list, err := store.PendingReviews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != pending.ID {
		t.Fatalf("pending=%+v, want only the unresolved review", list)
	}
}

func TestDeletingARunRemovesItsReview(t *testing.T) {
	db, store := testDB(t)
	ctx := context.Background()
	project, run := seedReviewFixture(t, store)
	now := time.Now().UTC()
	if _, err := store.CreateReview(ctx, models.RunReview{ProjectID: project.ID, RunID: run.ID, CheckpointHash: "abc123", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	if _, err := db.SQL.ExecContext(ctx, "DELETE FROM runs WHERE id=?", run.ID); err != nil {
		t.Fatal(err)
	}

	_, ok, err := store.Review(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("deleting the run must cascade-delete its review")
	}
}
