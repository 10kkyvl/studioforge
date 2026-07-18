package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
)

// A run sits in the queue whenever the concurrency ceilings are saturated, and
// that is exactly when a user is most likely to press Stop. Cancel used to only
// look at active runs, so stopping a queued run failed and the run started
// anyway once a slot freed up.
func TestCancelQueuedRun(t *testing.T) {
	cases := []struct {
		name       string
		unknownRun bool
		wantErr    bool
		wantStatus string
	}{
		{name: "queued run cancels straight to the terminal state", wantStatus: "cancelled"},
		{name: "unknown run is still rejected", unknownRun: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager, provider, store, ctx := newHarness(t)
			// One slot everywhere, held by a run that never finishes, so the
			// next submission is guaranteed to stay queued.
			manager.SetLimits(1, 1, 1, 1)
			if _, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1, Scenario: "hang"}); err != nil {
				t.Fatal(err)
			}
			waitForRequest(t, provider)
			queued, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-eng", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
			if err != nil {
				t.Fatal(err)
			}
			waitStatus(t, store, queued.ID, "queued", 2*time.Second)

			target := queued.ID
			if tc.unknownRun {
				target = "run-that-does-not-exist"
			}
			switch err := manager.Cancel(ctx, target); {
			case tc.wantErr && err == nil:
				t.Fatalf("Cancel(%q) succeeded; an unknown run must still be reported", target)
			case !tc.wantErr && err != nil:
				t.Fatalf("Cancel of a queued run failed: %v", err)
			}
			if tc.wantErr {
				// The queued run must survive a cancel aimed at something else.
				waitStatus(t, store, queued.ID, "queued", time.Second)
				return
			}
			waitStatus(t, store, queued.ID, tc.wantStatus, 2*time.Second)
			// The job must be gone from the queue, not merely marked: if it were
			// still there it would start as soon as the blocker released a slot.
			manager.mu.Lock()
			_, stillQueued := manager.queue.remove(queued.ID)
			manager.mu.Unlock()
			if stillQueued {
				t.Fatal("cancelled run is still in the queue and would start later")
			}
			assertCancelledEvent(t, store, queued.ID)
		})
	}
}

// The status event is what the chat UI keys off; a cancelled run that only
// changed rows in the database would leave the UI waiting on it forever.
func assertCancelledEvent(t *testing.T, store *database.Store, runID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		list, err := store.EventsAfter(context.Background(), 0, "demo-obby", runID, 200)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range list {
			payload, ok := event.Payload.(map[string]any)
			if event.Type == "status" && ok && payload["status"] == "cancelled" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("cancelling a queued run never emitted a status:cancelled event")
}
