package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/resources"
)

type ReviewGate interface {
	Open(ctx context.Context, runID, projectID, checkpoint string, expires time.Time) error
}

func (m *Manager) SetReviewGate(g ReviewGate) {
	m.mu.Lock()
	m.reviewGate = g
	m.mu.Unlock()
}

type ReviewGateExpiry func() time.Duration

func (m *Manager) SetReviewGateExpiry(f ReviewGateExpiry) {
	m.mu.Lock()
	m.reviewExpiry = f
	m.mu.Unlock()
}

const defaultReviewGateExpiry = 24 * time.Hour

func (m *Manager) reviewGateExpiryDuration() time.Duration {
	m.mu.Lock()
	f := m.reviewExpiry
	m.mu.Unlock()
	if f == nil {
		return defaultReviewGateExpiry
	}
	if d := f(); d > 0 {
		return d
	}
	return defaultReviewGateExpiry
}

func (m *Manager) openReviewGate(ctx context.Context, j *Job, lease *resources.Handle) {
	m.mu.Lock()
	gate := m.reviewGate
	m.mu.Unlock()
	if gate == nil {
		m.fail(ctx, j, "review gate is enabled for this agent but no review gate is configured")
		return
	}
	held, err := lease.Transfer("review:" + j.RunID)
	if err != nil {
		m.fail(ctx, j, "review gate could not hold the project lease: "+err.Error())
		return
	}
	expires := time.Now().Add(m.reviewGateExpiryDuration())
	if err := gate.Open(ctx, j.RunID, j.ProjectID, j.BaseCheckpoint, expires); err != nil {
		held.Release()
		m.fail(ctx, j, "review gate failed to open: "+err.Error())
		return
	}
	m.ResumeReviewLease(j.RunID, j.ProjectID, held, expires)
	if err := m.transition(ctx, j, "running", "waiting_decision", "review", "", ""); err != nil {
		return
	}
	m.emit(models.Run{ID: j.RunID, ProjectID: j.ProjectID}, j.AgentID, "review", "scheduler.review", map[string]any{
		"checkpoint": j.BaseCheckpoint,
		"expiresAt":  expires,
	})
}

func (m *Manager) ReleaseReviewLease(runID, projectID string) {
	m.leases.ReleaseOwned("review:"+runID, []string{"project:" + projectID + ":write"})
}

func (m *Manager) ResumeReviewLease(runID, projectID string, held *resources.Handle, expires time.Time) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.holdReviewLease(runID, projectID, held, expires)
	}()
}

func (m *Manager) holdReviewLease(runID, projectID string, held *resources.Handle, expires time.Time) {
	interval := validationHeartbeatInterval(m.leases.TTL())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			held.Release()
			return
		case now := <-ticker.C:
			review, ok, err := m.store.Review(context.Background(), runID)
			if err != nil {
				slog.Error("failed to check review status while holding its project lease", "run_id", runID, "project_id", projectID, "error", err)
				continue
			}
			if !ok || review.Status != "pending" {
				held.Release()
				return
			}
			if !now.Before(expires) {
				if _, err := m.store.ExpireReviews(context.Background(), now); err != nil {
					slog.Error("failed to expire the review while releasing its project lease", "run_id", runID, "project_id", projectID, "error", err)
				}
				held.Release()
				return
			}
			if err := held.Heartbeat(); err != nil {
				slog.Error("lost the project lease while holding it for review", "run_id", runID, "project_id", projectID, "error", err)
				held.Release()
				return
			}
		}
	}
}
