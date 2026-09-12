package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

func TestStudioJournalSurvivesRetentionAndIsRunIsolated(t *testing.T) {
	db, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	run := createRunWithStatus(t, store, ctx, "completed", time.Now().Add(-200*24*time.Hour))
	other, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-arena", AgentID: "demo-arena-orch", Provider: "mock", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	journal := store.StudioRecorder(run.ID)
	id, err := journal.Start(ctx, "multi_edit", map[string]any{"path": "ServerScriptService.Main", "new_source": "DO NOT STORE THIS"})
	if err != nil {
		t.Fatal(err)
	}
	if err = journal.Finish(ctx, id, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if err = store.StudioRecorder(other.ID).Finish(ctx, id, "error"); err == nil {
		t.Fatal("cross-run outcome accepted")
	}
	appendEvent(t, store, ctx, run, "tool", "tool.use", map[string]any{"tool": "multi_edit"})
	if n, err := store.PruneEvents(ctx, 90); err != nil || n != 1 {
		t.Fatalf("prune %d %v", n, err)
	}
	changes, err := store.StudioChanges(ctx, run.ID)
	if err != nil || len(changes) != 1 || changes[0].Status != "succeeded" || changes[0].Target != "ServerScriptService.Main" {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
	if otherChanges, err := store.StudioChanges(ctx, other.ID); err != nil || len(otherChanges) != 0 {
		t.Fatalf("other=%+v %v", otherChanges, err)
	}
	var props string
	if err := db.SQL.QueryRowContext(ctx, "SELECT properties FROM studio_changes WHERE call_id=?", id).Scan(&props); err != nil || strings.Contains(props, "DO NOT STORE") {
		t.Fatalf("props=%s err=%v", props, err)
	}
	if _, err := store.StudioRecorder("missing").Start(ctx, "execute_luau", nil); err == nil {
		t.Fatal("nonexistent run accepted")
	}
}

func TestShimDatabaseConnectionDoesNotMigrate(t *testing.T) {
	db, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	run := createRunWithStatus(t, store, ctx, "running", time.Time{})
	journal, closeJournal, err := OpenStudioJournal(ctx, db.Path, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer closeJournal()
	id, err := journal.Start(ctx, "insert_asset", map[string]any{"path": "Workspace.Model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Finish(ctx, id, "unknown"); err != nil {
		t.Fatal(err)
	}
	changes, err := store.StudioChanges(ctx, run.ID)
	if err != nil || len(changes) != 1 || changes[0].Status != "unknown" {
		t.Fatalf("%+v %v", changes, err)
	}
	if _, _, err := OpenStudioJournal(ctx, filepath.Join(t.TempDir(), "absent.db"), run.ID); err == nil {
		t.Fatal("created absent database")
	}
}
