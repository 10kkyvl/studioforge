package database

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

func createRunWithStatus(t *testing.T, store *Store, ctx context.Context, status string, finishedAt time.Time) models.Run {
	t.Helper()
	run, ok, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced", Status: status}, "")
	if err != nil || !ok {
		t.Fatalf("create run: %v ok=%v", err, ok)
	}
	if !finishedAt.IsZero() {
		if _, err := store.db.SQL.ExecContext(ctx, "UPDATE runs SET finished_at=? WHERE id=?", finishedAt.UTC().Format(time.RFC3339Nano), run.ID); err != nil {
			t.Fatal(err)
		}
	}
	return run
}

func appendEvent(t *testing.T, store *Store, ctx context.Context, run models.Run, eventType, rawType string, payload map[string]any) models.RunEvent {
	t.Helper()
	appended, err := store.AppendEvents(ctx, []models.RunEvent{{ProjectID: run.ProjectID, RunID: run.ID, AgentID: run.AgentID, Type: eventType, RawType: rawType, Payload: payload}})
	if err != nil {
		t.Fatal(err)
	}
	return appended[0]
}

func countEvents(t *testing.T, store *Store, ctx context.Context, runID string) int {
	t.Helper()
	var n int
	if err := store.db.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM run_events WHERE run_id=?", runID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPruneEventsDeletesOldTerminalRunVerboseEvents(t *testing.T) {
	store, ctx := newThreadStore(t)
	old := time.Now().Add(-200 * 24 * time.Hour)
	run := createRunWithStatus(t, store, ctx, "completed", old)
	appendEvent(t, store, ctx, run, "tool", "tool.use", map[string]any{"tool": "Read"})
	appendEvent(t, store, ctx, run, "status", "run.status", map[string]any{"phase": "starting"})

	deleted, err := store.PruneEvents(ctx, 90)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted=%d, want 2", deleted)
	}
	if got := countEvents(t, store, ctx, run.ID); got != 0 {
		t.Fatalf("events remaining=%d, want 0", got)
	}
}

func TestPruneEventsKeepsRecentTerminalRunEvents(t *testing.T) {
	store, ctx := newThreadStore(t)
	recent := time.Now().Add(-1 * time.Hour)
	run := createRunWithStatus(t, store, ctx, "completed", recent)
	appendEvent(t, store, ctx, run, "tool", "tool.use", map[string]any{"tool": "Read"})

	deleted, err := store.PruneEvents(ctx, 90)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("deleted=%d, want 0", deleted)
	}
	if got := countEvents(t, store, ctx, run.ID); got != 1 {
		t.Fatalf("events remaining=%d, want 1", got)
	}
}

func TestPruneEventsKeepsActiveRunEvents(t *testing.T) {
	store, ctx := newThreadStore(t)
	old := time.Now().Add(-200 * 24 * time.Hour)
	run := createRunWithStatus(t, store, ctx, "running", time.Time{})
	appendEvent(t, store, ctx, run, "tool", "tool.use", map[string]any{"tool": "Read"})
	if _, err := store.db.SQL.ExecContext(ctx, "UPDATE runs SET created_at=? WHERE id=?", old.UTC().Format(time.RFC3339Nano), run.ID); err != nil {
		t.Fatal(err)
	}

	deleted, err := store.PruneEvents(ctx, 90)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("deleted=%d, want 0 (run is still active)", deleted)
	}
	if got := countEvents(t, store, ctx, run.ID); got != 1 {
		t.Fatalf("events remaining=%d, want 1", got)
	}
}

func TestPruneEventsPreservesChatHistory(t *testing.T) {
	store, ctx := newThreadStore(t)
	thread, err := store.EnsureDefaultThread(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-200 * 24 * time.Hour)
	run, ok, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced", Status: "completed", ThreadID: thread.ID, PromptSnapshot: "Build me a lobby"}, "")
	if err != nil || !ok {
		t.Fatalf("create run: %v ok=%v", err, ok)
	}
	if _, err := store.db.SQL.ExecContext(ctx, "UPDATE runs SET finished_at=? WHERE id=?", old.UTC().Format(time.RFC3339Nano), run.ID); err != nil {
		t.Fatal(err)
	}
	appendEvent(t, store, ctx, run, "message", "openrouter.message.partial", map[string]any{"text": "thinking..."})
	appendEvent(t, store, ctx, run, "message", "assistant.final", map[string]any{"text": "Lobby is built."})
	appendEvent(t, store, ctx, run, "tool", "tool.use", map[string]any{"tool": "Read"})
	appendEvent(t, store, ctx, run, "status", "run.status", map[string]any{"phase": "starting"})

	before, err := store.ThreadMessages(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := store.PruneEvents(ctx, 90)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("deleted=%d, want 3 (partial message + tool + status, keeping the final message)", deleted)
	}

	after, err := store.ThreadMessages(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("chat history changed after prune: before=%d after=%d", len(before), len(after))
	}
	foundFinal := false
	for _, m := range after {
		if m.Text == "Lobby is built." {
			foundFinal = true
		}
	}
	if !foundFinal {
		t.Fatalf("expected the final assistant message to survive pruning: %+v", after)
	}
}

func TestPruneEventsRetentionZeroIsNoop(t *testing.T) {
	store, ctx := newThreadStore(t)
	old := time.Now().Add(-200 * 24 * time.Hour)
	run := createRunWithStatus(t, store, ctx, "completed", old)
	appendEvent(t, store, ctx, run, "tool", "tool.use", map[string]any{"tool": "Read"})

	deleted, err := store.PruneEvents(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("deleted=%d, want 0 (retention disabled)", deleted)
	}
	if got := countEvents(t, store, ctx, run.ID); got != 1 {
		t.Fatalf("events remaining=%d, want 1", got)
	}
}

func TestPruneEventsBatchesLargeBacklogs(t *testing.T) {
	store, ctx := newThreadStore(t)
	old := time.Now().Add(-200 * 24 * time.Hour)
	run := createRunWithStatus(t, store, ctx, "completed", old)
	const total = eventPruneBatchSize + 137
	events := make([]models.RunEvent, 0, total)
	for i := 0; i < total; i++ {
		events = append(events, models.RunEvent{ProjectID: run.ProjectID, RunID: run.ID, AgentID: run.AgentID, Type: "tool", RawType: "tool.use", Payload: map[string]any{"i": i}})
	}
	if _, err := store.AppendEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	if got := countEvents(t, store, ctx, run.ID); got != total {
		t.Fatalf("seeded events=%d, want %d", got, total)
	}

	deleted, err := store.PruneEvents(ctx, 90)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != int64(total) {
		t.Fatalf("deleted=%d, want %d", deleted, total)
	}
	if got := countEvents(t, store, ctx, run.ID); got != 0 {
		t.Fatalf("events remaining=%d, want 0", got)
	}
}

func TestPruneEventsInterruptionSafeAndResumable(t *testing.T) {
	store, ctx := newThreadStore(t)
	old := time.Now().Add(-200 * 24 * time.Hour)
	run := createRunWithStatus(t, store, ctx, "completed", old)
	const total = eventPruneBatchSize*2 + 10
	events := make([]models.RunEvent, 0, total)
	for i := 0; i < total; i++ {
		events = append(events, models.RunEvent{ProjectID: run.ProjectID, RunID: run.ID, AgentID: run.AgentID, Type: "tool", RawType: "tool.use", Payload: map[string]any{"i": i}})
	}
	if _, err := store.AppendEvents(ctx, events); err != nil {
		t.Fatal(err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	deleted, err := store.PruneEvents(cancelCtx, 90)
	if err == nil {
		t.Fatal("expected an error from an already-cancelled context")
	}
	if deleted != 0 {
		t.Fatalf("deleted=%d before any batch should run, want 0", deleted)
	}
	if got := countEvents(t, store, ctx, run.ID); got != total {
		t.Fatalf("events remaining after a cancelled prune=%d, want all %d preserved", got, total)
	}

	deleted, err = store.PruneEvents(ctx, 90)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != int64(total) {
		t.Fatalf("resumed prune deleted=%d, want %d", deleted, total)
	}
	if got := countEvents(t, store, ctx, run.ID); got != 0 {
		t.Fatalf("events remaining=%d, want 0", got)
	}
	if err := store.db.Integrity(ctx); err != nil {
		t.Fatalf("integrity check failed after prune: %v", err)
	}
}

func TestPruneEventsIntegrityCheckAfterPrune(t *testing.T) {
	store, ctx := newThreadStore(t)
	old := time.Now().Add(-200 * 24 * time.Hour)
	run := createRunWithStatus(t, store, ctx, "failed", old)
	for i := 0; i < 50; i++ {
		appendEvent(t, store, ctx, run, "tool", "tool.use", map[string]any{"tool": "Read"})
	}
	if _, err := store.PruneEvents(ctx, 90); err != nil {
		t.Fatal(err)
	}
	if err := store.db.Integrity(ctx); err != nil {
		t.Fatalf("integrity check failed after prune: %v", err)
	}
}

func TestPruneEventsOnceLogsOnlyAggregates(t *testing.T) {
	store, ctx := newThreadStore(t)
	old := time.Now().Add(-200 * 24 * time.Hour)
	run := createRunWithStatus(t, store, ctx, "completed", old)
	secretText := "very-secret-payload-marker-xyz"
	appendEvent(t, store, ctx, run, "tool", "tool.use", map[string]any{"detail": secretText})

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(previous)

	store.pruneEventsOnce(ctx, func() int { return 90 })

	out := buf.String()
	if strings.Contains(out, secretText) {
		t.Fatalf("maintenance log leaked event payload text: %s", out)
	}
	if !strings.Contains(out, "deleted=1") {
		t.Fatalf("expected an aggregate deleted count in the maintenance log: %s", out)
	}
	if got := countEvents(t, store, ctx, run.ID); got != 0 {
		t.Fatalf("events remaining=%d, want 0", got)
	}
}
