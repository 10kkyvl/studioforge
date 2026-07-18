package memory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/10kkyvl/studioforge/internal/database"
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
