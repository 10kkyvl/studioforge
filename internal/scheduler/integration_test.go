package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/mock"
	"github.com/10kkyvl/studioforge/internal/resources"
)

func TestMultiProjectIntegrationRecoveryAndIsolation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "integration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := database.NewStore(db)
	data := t.TempDir()
	if err := store.SeedDemo(ctx, data); err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub(store)
	defer hub.Close()
	leases := resources.NewManager(time.Second)
	defer leases.Close()
	provider := mock.New()
	provider.StepDelay = 30 * time.Millisecond
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider})
	defer manager.Close(context.Background())
	projects := []string{"demo-obby", "demo-tycoon", "demo-arena"}
	ids := []string{}
	for _, project := range projects {
		run, created, err := manager.Submit(ctx, Job{ProjectID: project, AgentID: project + "-orch", Provider: "mock", Model: "balanced", WorkingDirectory: data, MaxBudget: 1})
		if err != nil || !created {
			t.Fatalf("submit %s created=%v err=%v", project, created, err)
		}
		ids = append(ids, run.ID)
	}
	// Use a distinct model so the model concurrency ceiling cannot keep this run
	// queued until the first writer has already released the project lease.
	blocked, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-eng", Provider: "mock", Model: "writer", WorkingDirectory: data, MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	sawWaiting := false
	for time.Now().Before(deadline) {
		for _, id := range []string{ids[0], blocked.ID} {
			run, _ := store.Run(ctx, id)
			if run.Status == "waiting_resources" {
				sawWaiting = true
				break
			}
		}
		if sawWaiting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !sawWaiting {
		t.Fatal("neither same-project writer visibly waited for the project lease")
	}
	for _, id := range append(ids, blocked.ID) {
		waitStatus(t, store, id, "completed", 4*time.Second)
	}
	for i, id := range ids {
		eventsList, err := store.EventsAfter(ctx, 0, projects[i], id, 100)
		if err != nil || len(eventsList) == 0 {
			t.Fatalf("events project=%s run=%s count=%d err=%v", projects[i], id, len(eventsList), err)
		}
		for _, event := range eventsList {
			if event.ProjectID != projects[i] || event.RunID != id {
				t.Fatalf("cross-project event=%+v", event)
			}
		}
	}
	crashed, _, err := manager.Submit(ctx, Job{ProjectID: "demo-arena", AgentID: "demo-arena-eng", Provider: "mock", Model: "fast", WorkingDirectory: data, Scenario: "crash", MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, crashed.ID, "failed", 3*time.Second)
	hanging, _, err := manager.Submit(ctx, Job{ProjectID: "demo-tycoon", AgentID: "demo-tycoon-eng", Provider: "mock", Model: "fast", WorkingDirectory: data, Scenario: "hang", MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, hanging.ID, "running", 2*time.Second)
	if err := manager.Pause(ctx, hanging.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, hanging.ID, "paused", 2*time.Second)
	if err := manager.Resume(ctx, hanging.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, hanging.ID, "running", 2*time.Second)
	if err := manager.Cancel(ctx, hanging.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, hanging.ID, "cancelled", 2*time.Second)
	_, err = db.SQL.Exec("UPDATE runs SET status='running' WHERE id=?", ids[0])
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := store.RecoverInterrupted(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
}
func waitStatus(t *testing.T, store *database.Store, id, status string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		run, err := store.Run(context.Background(), id)
		if err == nil {
			last = run.Status
			if last == status {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s status=%s wanted=%s", id, last, status)
}
