package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

func TestCreateCheckpointAndCheckpointForRunRoundTrip(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "claude", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := models.Checkpoint{ProjectID: "demo-obby", RunID: run.ID, CommitHash: "abc123", Branch: "main", Label: "StudioForge checkpoint before agent run"}
	if err := store.CreateCheckpoint(ctx, checkpoint); err != nil {
		t.Fatal(err)
	}
	got, err := store.CheckpointForRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "demo-obby" || got.RunID != run.ID || got.CommitHash != "abc123" || got.Branch != "main" || got.Label != checkpoint.Label {
		t.Fatalf("checkpoint=%+v", got)
	}
	if got.ID == "" || got.CreatedAt.IsZero() {
		t.Fatalf("checkpoint missing generated fields: %+v", got)
	}
}

func TestCheckpointsForProjectRoundTripNewestFirst(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "claude", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	first := models.Checkpoint{ProjectID: "demo-obby", RunID: run.ID, CommitHash: "commit-1", Branch: "main", Label: "first", CreatedAt: time.Now().UTC().Add(-time.Minute)}
	if err := store.CreateCheckpoint(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := models.Checkpoint{ProjectID: "demo-obby", RunID: run.ID, CommitHash: "commit-2", Branch: "main", Label: "second", CreatedAt: time.Now().UTC()}
	if err := store.CreateCheckpoint(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, err := store.CheckpointsForProject(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("checkpoints=%d, want 2: %+v", len(got), got)
	}
	if got[0].CommitHash != "commit-2" || got[1].CommitHash != "commit-1" {
		t.Fatalf("expected newest-first order, got %+v", got)
	}
}

func TestCheckpointsForProjectWithNoneReturnsEmptyNotNil(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	got, err := store.CheckpointsForProject(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected a non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("checkpoints=%d, want 0", len(got))
	}
}

func TestCheckpointForRunWithNoneReturnsCleanNotFound(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CheckpointForRun(ctx, run.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}
