package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/memory"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/mock"
	"github.com/10kkyvl/studioforge/internal/resources"
)

func TestCorrectionJobCarriesMemoryProvenance(t *testing.T) {
	parent := &Job{RunID: "parent", ProjectID: "demo-obby", MemoryEntryIDs: []string{"memory-1", "memory-2"}}
	correction := buildCorrectionJob(parent, "session", ValidationResult{Outcome: ValidationFailed})
	if len(correction.MemoryEntryIDs) != 2 || correction.MemoryEntryIDs[0] != "memory-1" || correction.MemoryEntryIDs[1] != "memory-2" {
		t.Fatalf("correction memory provenance=%v", correction.MemoryEntryIDs)
	}
	correction.MemoryEntryIDs[0] = "changed"
	if parent.MemoryEntryIDs[0] != "memory-1" {
		t.Fatal("correction must copy memory provenance instead of aliasing the parent slice")
	}
}

func TestCorrectionRunRecordsMemoryProvenanceBeforeAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "memory-provenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := database.NewStore(db)
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	memoryStore := memory.New(db)
	if err := memoryStore.Put(ctx, memory.Entry{ID: "memory-correction", ProjectID: "demo-obby", Content: "keep the server contract", Summary: "keep the server contract", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub(store)
	t.Cleanup(hub.Close)
	leases := resources.NewManager(time.Second)
	t.Cleanup(leases.Close)
	provider := mock.New()
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider})
	manager.SetMemory(memoryStore)
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	parent, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced", Status: "completed", Phase: "verified"}, "")
	if err != nil {
		t.Fatal(err)
	}
	parentJob := &Job{RunID: parent.ID, ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", Prompt: "original", MemoryEntryIDs: []string{"memory-correction"}}
	manager.scheduleCorrection(ctx, parentJob, "session", ValidationResult{Outcome: ValidationFailed, Errors: []string{"broken"}})
	runs, err := store.ListRuns(ctx, "demo-obby", 20)
	if err != nil {
		t.Fatal(err)
	}
	var correction models.Run
	for _, run := range runs {
		if run.ParentRunID == parent.ID {
			correction = run
			break
		}
	}
	if correction.ID == "" {
		t.Fatal("correction run was not created")
	}
	entries, err := memoryStore.List(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.ID == "memory-correction" {
			if !entry.Injected {
				t.Fatalf("correction memory entry was not marked injected: %+v", entry)
			}
			return
		}
	}
	t.Fatal("memory entry disappeared")
}
