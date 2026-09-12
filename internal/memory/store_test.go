package memory

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func TestMemoryUpdatePersistsDerivedSummary(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.NewStore(db).SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	if err := store.Put(ctx, Entry{ID: "summary-memory", ProjectID: "demo-obby", Content: "old content", Summary: "old summary", Source: "run"}); err != nil {
		t.Fatal(err)
	}
	content := "New durable contract\nImplementation detail"
	if _, err := store.Update(ctx, "summary-memory", &content, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Summary != "New durable contract" || entries[0].Source != "edited" {
		t.Fatalf("updated entry=%+v", entries)
	}
}

func TestSummaryForContentKeepsUnicodeBoundaries(t *testing.T) {
	content := strings.Repeat("я", 160)
	if got := summaryForContent(content); got != strings.Repeat("я", 140) {
		t.Fatalf("summary length/content mismatch: got %d runes", len([]rune(got)))
	}
}

func TestMemorySearchNaturalLanguageIsBoundedAndSafe(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.NewStore(db).SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	if err := store.Put(ctx, Entry{ID: "search-server", ProjectID: "demo-obby", Content: "server validation contract", Summary: "server contract", Source: "test", Importance: .9}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, Entry{ID: "search-client", ProjectID: "demo-obby", Content: "client rendering contract", Summary: "client contract", Source: "test", Importance: .8}); err != nil {
		t.Fatal(err)
	}
	query := `Please find "server" OR validation; DROP TABLE memory_entries; ` + strings.Repeat("noise ", 100)
	results, err := store.Search(ctx, "demo-obby", query, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "search-server" {
		t.Fatalf("FTS natural language results=%+v", results)
	}
	db.FTS5 = false
	results, err = store.Search(ctx, "demo-obby", query, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "search-server" {
		t.Fatalf("LIKE natural language results=%+v", results)
	}
}

func TestMemorySearchReservesRelevantUnpinnedSlots(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.NewStore(db).SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	for i := 0; i < 5; i++ {
		if err := store.Put(ctx, Entry{ID: fmt.Sprintf("pinned-%d", i), ProjectID: "demo-obby", Content: fmt.Sprintf("pinned note %d", i), Summary: "pinned", Source: "test", Pinned: true, Importance: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Put(ctx, Entry{ID: "relevant-unpinned", ProjectID: "demo-obby", Content: "server validation rule", Summary: "server", Source: "test", Importance: .1}); err != nil {
		t.Fatal(err)
	}
	results, err := store.Search(ctx, "demo-obby", "server", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatalf("reserved results=%+v", results)
	}
	found := false
	for _, entry := range results {
		if entry.ID == "relevant-unpinned" {
			found = true
		}
	}
	if !found {
		t.Fatalf("relevant unpinned entry was crowded out: %+v", results)
	}
}

func TestMemoryAutoDedupAndRetention(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.NewStore(db).SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	if err := store.Put(ctx, Entry{ID: "auto-duplicate-one", ProjectID: "demo-obby", Content: "same outcome", Source: "run"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, Entry{ID: "auto-duplicate-two", ProjectID: "demo-obby", Content: " same outcome ", Source: "run"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < defaultAutoRetentionLimit+3; i++ {
		if err := store.Put(ctx, Entry{ID: fmt.Sprintf("auto-%03d", i), ProjectID: "demo-obby", Content: fmt.Sprintf("unique outcome %03d", i), Source: "run", CreatedAt: time.Unix(int64(i+1), 0).UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := store.List(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != defaultAutoRetentionLimit {
		t.Fatalf("retained %d entries, want %d", len(entries), defaultAutoRetentionLimit)
	}
	for _, entry := range entries {
		if entry.ID == "auto-duplicate-two" {
			t.Fatal("duplicate auto memory was inserted")
		}
	}
	if _, err := store.Update(ctx, "auto-050", strptr("manually curated outcome"), nil); err != nil {
		t.Fatal(err)
	}
	pinned := true
	if _, err := store.Update(ctx, "auto-051", nil, &pinned); err != nil {
		t.Fatal(err)
	}
	for i := defaultAutoRetentionLimit + 3; i < 2*defaultAutoRetentionLimit+6; i++ {
		if err := store.Put(ctx, Entry{ID: fmt.Sprintf("later-%03d", i), ProjectID: "demo-obby", Content: fmt.Sprintf("later outcome %03d", i), Source: "run", CreatedAt: time.Unix(int64(i+100), 0).UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err = store.List(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	foundEdited, foundPinned := false, false
	for _, entry := range entries {
		if entry.ID == "auto-051" && entry.Pinned {
			foundPinned = true
		}
		if entry.ID == "auto-050" && entry.Source == "edited" {
			foundEdited = true
		}
	}
	if !foundEdited || !foundPinned {
		t.Fatal("manual edit or pinned entry was removed by automatic retention")
	}
}

func strptr(value string) *string { return &value }

func TestEditedUnicodeSummaryAndUnpinnedSearchFill(t *testing.T) {
	ctx := t.Context()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "utf8.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.NewStore(db).SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	for i := 0; i < 5; i++ {
		if err := store.Put(ctx, Entry{ID: fmt.Sprint(i), ProjectID: "demo-obby", Content: "unicode contract", Source: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, fts := range []bool{db.FTS5, false} {
		db.FTS5 = fts
		for _, limit := range []int{1, 5} {
			got, err := store.Search(ctx, "demo-obby", "contract", limit)
			if err != nil || len(got) != limit {
				t.Fatalf("fts=%v limit=%d got=%d err=%v", fts, limit, len(got), err)
			}
		}
	}
	text := "a" + strings.Repeat("я", 100)
	if _, err := store.Update(ctx, "0", &text, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !utf8.ValidString(e.Summary) {
			t.Fatalf("broken UTF8 summary: %q", e.Summary)
		}
	}
}
