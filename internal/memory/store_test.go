package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/models"
)

func TestMemoryRetrievalIsolationAndFallback(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := database.NewStore(db)
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	memory := New(db)
	if err := memory.Put(ctx, Entry{ProjectID: "demo-obby", Scope: "project", Content: "The grappling hook contract uses server validation", Summary: "grappling contract", Source: "test", Confidence: .9, Importance: .8}); err != nil {
		t.Fatal(err)
	}
	if err := memory.Put(ctx, Entry{ProjectID: "demo-arena", Scope: "project", Content: "arena only secret", Summary: "arena", Source: "test", Confidence: .9, Importance: .8}); err != nil {
		t.Fatal(err)
	}
	results, err := memory.Search(ctx, "demo-obby", "grappling", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ProjectID != "demo-obby" {
		t.Fatalf("results=%+v", results)
	}
	db.FTS5 = false
	fallback, err := memory.Search(ctx, "demo-obby", "server validation", 10)
	if err != nil || len(fallback) != 1 {
		t.Fatalf("fallback=%+v err=%v", fallback, err)
	}
}

func TestMemoryManagementPinInjectionAndDeletion(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := database.NewStore(db)
	root := t.TempDir()
	if err := store.SeedDemo(ctx, root); err != nil {
		t.Fatal(err)
	}
	memoryStore := New(db)
	sourceRun, _, err := store.CreateRun(ctx, models.Run{ID: "z-older-run", ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "default"}, "")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour)
	first := Entry{ID: "mem-first", ProjectID: "demo-obby", Content: "first content", Summary: "first", Source: "test", Importance: .1, CreatedAt: old}
	first.RunID = sourceRun.ID
	second := Entry{ID: "mem-second", ProjectID: "demo-obby", Content: "second content", Summary: "second", Source: "test", Importance: .9}
	if err := memoryStore.Put(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.Put(ctx, second); err != nil {
		t.Fatal(err)
	}
	pinned := true
	updated, err := memoryStore.Update(ctx, first.ID, nil, &pinned)
	if err != nil || !updated.Pinned {
		t.Fatalf("pin update=%+v err=%v", updated, err)
	}
	content := "Corrected server contract\nSecond detail"
	updated, err = memoryStore.Update(ctx, first.ID, &content, nil)
	if err != nil || updated.Summary != "Corrected server contract" || !updated.Pinned {
		t.Fatalf("content update=%+v err=%v", updated, err)
	}
	results, err := memoryStore.Search(ctx, "demo-obby", "content", 1)
	if err != nil || len(results) != 1 || results[0].ID != first.ID {
		t.Fatalf("pinned search=%+v err=%v", results, err)
	}
	results, err = memoryStore.Search(ctx, "demo-obby", "unrelated-query", 1)
	if err != nil || len(results) != 1 || results[0].ID != first.ID {
		t.Fatalf("pinned unrelated search=%+v err=%v", results, err)
	}
	db.FTS5 = false
	results, err = memoryStore.Search(ctx, "demo-obby", "unrelated-query", 1)
	if err != nil || len(results) != 1 || results[0].ID != first.ID {
		t.Fatalf("pinned fallback search=%+v err=%v", results, err)
	}
	run, _, err := store.CreateRun(ctx, models.Run{ID: "a-newer-run", ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "default"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Force a timestamp collision on a coarse clock, with IDs sorting opposite
	// to insertion order. The later inserted run must still be the latest run
	// whose injections are reflected by List.
	if _, err := db.SQL.ExecContext(ctx, `UPDATE runs SET created_at=(SELECT created_at FROM runs WHERE id=?) WHERE id=?`, sourceRun.ID, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.RecordInjection(ctx, run.ID, []string{first.ID}); err != nil {
		t.Fatal(err)
	}
	otherRun, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-arena", AgentID: "demo-arena-orch", Provider: "mock", ModelAlias: "default"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.RecordInjection(ctx, otherRun.ID, []string{first.ID}); err == nil {
		t.Fatal("cross-project memory injection must be rejected")
	}
	listed, err := memoryStore.List(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	var found Entry
	for _, item := range listed {
		if item.ID == first.ID {
			found = item
		}
	}
	if !found.Injected || !found.Pinned {
		t.Fatalf("listed source=%+v", found)
	}
	if err := memoryStore.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if kept, err := store.Run(ctx, sourceRun.ID); err != nil || kept.ID != sourceRun.ID {
		t.Fatalf("source run was changed by memory deletion: %+v err=%v", kept, err)
	}
	if n, err := memoryStore.Clear(ctx, "demo-obby"); err != nil || n != 1 {
		t.Fatalf("clear n=%d err=%v", n, err)
	}
}
