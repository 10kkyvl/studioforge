package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/resources"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

type reviewGateAdapter struct{ store *database.Store }

func (a *reviewGateAdapter) Open(ctx context.Context, runID, projectID, checkpoint string, expires time.Time) error {
	_, err := a.store.CreateReview(ctx, models.RunReview{RunID: runID, ProjectID: projectID, CheckpointHash: checkpoint, ExpiresAt: expires})
	return err
}

func recoverPendingReviews(ctx context.Context, store *database.Store, leases *resources.Manager, sched *scheduler.Manager) {
	now := time.Now().UTC()
	if _, err := store.ExpireReviews(ctx, now); err != nil {
		slog.Warn("failed to expire stale run reviews at startup", "error", err)
	}
	pending, err := store.PendingReviews(ctx)
	if err != nil {
		slog.Warn("failed to list pending run reviews at startup", "error", err)
		return
	}
	for _, review := range pending {
		acquireCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		handle, err := leases.Acquire(acquireCtx, "review:"+review.RunID, []string{"project:" + review.ProjectID + ":write"})
		cancel()
		if err != nil {
			slog.Warn("could not reacquire the project lease for a pending run review at startup; leaving the project unprotected until it resolves", "review_id", review.ID, "run_id", review.RunID, "project_id", review.ProjectID, "error", err)
			continue
		}
		sched.ResumeReviewLease(review.RunID, review.ProjectID, handle, review.ExpiresAt)
	}
}
