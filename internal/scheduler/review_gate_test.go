package scheduler

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

func TestReviewGateFailsBeforeProviderWithoutCheckpoint(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	manager.SetReviewProposer(func(_ context.Context, req ReviewRequest) (ReviewProposal, error) {
		t.Fatal("review proposer must not run without a checkpoint")
		return ReviewProposal{}, nil
	})
	run, _, err := manager.Submit(ctx, Job{
		ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced",
		WorkingDirectory: t.TempDir(), MaxBudget: 1, ReviewBeforeApply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "failed", 2*time.Second)
	failure, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failure.Error == "" || !strings.Contains(failure.Error, "Git checkpoint") {
		t.Fatalf("failure=%q, want missing checkpoint error", failure.Error)
	}
}

func TestPendingReviewBlocksDurableProjectWriter(t *testing.T) {
	manager, provider, store, ctx := newHarness(t)
	run, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced", Status: "waiting_decision", Phase: "waiting_decision"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateDecision(ctx, models.Decision{ProjectID: "demo-obby", RunID: run.ID, Kind: "review_before_apply", Status: "pending", Summary: "pending review", Payload: `{"checkpoint":"base"}`}); err != nil {
		t.Fatal(err)
	}
	blocked, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-eng", Provider: "mock", Model: "fast", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, blocked.ID, "failed", 2*time.Second)
	if len(provider.requests()) != 0 {
		t.Fatal("provider started while a durable review was pending")
	}
}

func TestReviewGateHoldsWriterLeaseUntilResolution(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	manager.SetLimits(2, 2, 2, 2)
	manager.SetReviewProposer(func(_ context.Context, req ReviewRequest) (ReviewProposal, error) {
		return ReviewProposal{Pending: true, DecisionID: "decision"}, nil
	})
	run, _, err := manager.Submit(ctx, Job{
		ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced",
		WorkingDirectory: t.TempDir(), MaxBudget: 1, BaseCommit: "checkpoint", ReviewBeforeApply: true, ReviewTimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "waiting_decision", 2*time.Second)
	second, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-eng", Provider: "mock", Model: "fast", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, second.ID, "waiting_resources", 2*time.Second)
	// The old implementation returned after one heartbeat tick and released
	// the lease while the decision was still pending. Keep the review open over
	// two renewal intervals and prove the competing writer remains blocked.
	time.Sleep(1200 * time.Millisecond)
	stillWaiting, err := store.Run(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillWaiting.Status != "waiting_resources" {
		t.Fatalf("competing writer status after heartbeat renewals=%q, want waiting_resources", stillWaiting.Status)
	}
	if err := manager.ResolveReview(run.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "completed", 2*time.Second)
	finalRun, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalRun.Phase != "applied" {
		t.Fatalf("review-approved run phase=%q, want applied", finalRun.Phase)
	}
	// A second same-project run must become runnable once the review releases
	// the lease, proving the wait was the only blocker.
	waitStatus(t, store, second.ID, "completed", 2*time.Second)
}

func TestReviewGateTimeoutRejectsAndReleasesWriterLease(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	manager.SetLimits(2, 2, 2, 2)
	var timedOut atomic.Int32
	timeoutSeen := make(chan time.Duration, 1)
	manager.SetReviewProposer(func(_ context.Context, req ReviewRequest) (ReviewProposal, error) {
		timeoutSeen <- req.Timeout
		return ReviewProposal{Pending: true, DecisionID: "decision"}, nil
	})
	manager.SetReviewTimeoutHandler(func(context.Context, ReviewRequest) error {
		timedOut.Add(1)
		return nil
	})
	run, _, err := manager.Submit(ctx, Job{
		ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced",
		WorkingDirectory: t.TempDir(), MaxBudget: 1, BaseCommit: "checkpoint", ReviewBeforeApply: true, ReviewTimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-timeoutSeen:
		if got != time.Second {
			t.Fatalf("review timeout=%s, want 1s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("review proposer was not called")
	}
	waitStatus(t, store, run.ID, "failed", 3*time.Second)
	if timedOut.Load() != 1 {
		t.Fatalf("timeout handler calls=%d, want 1", timedOut.Load())
	}
	second, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-eng", Provider: "mock", Model: "fast", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, second.ID, "completed", 2*time.Second)
}

func TestCancelReviewAttemptsRejectionBeforeReleasingLease(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	manager.SetReviewProposer(func(context.Context, ReviewRequest) (ReviewProposal, error) {
		return ReviewProposal{Pending: true, DecisionID: "decision"}, nil
	})
	var rejected atomic.Int32
	manager.SetReviewTimeoutHandler(func(context.Context, ReviewRequest) error {
		rejected.Add(1)
		return nil
	})
	run, _, err := manager.Submit(ctx, Job{
		ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced",
		WorkingDirectory: t.TempDir(), MaxBudget: 1, BaseCommit: "checkpoint", ReviewBeforeApply: true, ReviewTimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "waiting_decision", 2*time.Second)
	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "cancelled", 2*time.Second)
	if rejected.Load() != 1 {
		t.Fatalf("review rejection attempts=%d, want 1", rejected.Load())
	}
}
