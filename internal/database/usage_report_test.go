package database

import (
	"github.com/10kkyvl/studioforge/internal/models"
	"reflect"
	"testing"
	"time"
)

func TestUsageReportRetainsAccountingAfterPruning(t *testing.T) {
	store, ctx := newThreadStore(t)
	old := time.Date(2025, 12, 31, 12, 0, 0, 0, time.UTC)
	run := createRunWithStatus(t, store, ctx, "completed", old)
	if err := store.SetRunUsage(ctx, run.ID, "", 1.5, models.TokenUsage{InputTokens: 40, OutputTokens: 12, CacheReadTokens: 8}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.SQL.ExecContext(ctx, "UPDATE usage_records SET recorded_at=? WHERE run_id=?", old.Format(time.RFC3339Nano), run.ID); err != nil {
		t.Fatal(err)
	}
	appendEvent(t, store, ctx, run, "tool", "tool.use", map[string]any{"tool": "Read"})
	before, err := store.UsageReport(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range before.Runs {
		if r.ID == run.ID {
			found = true
			if !r.Recorded || r.Cost != 1.5 || r.CacheReadTokens != 8 {
				t.Fatalf("run=%+v", r)
			}
		}
	}
	if !found {
		t.Fatal("run missing")
	}
	found = false
	for _, b := range before.Weeks {
		if b.Key == "2025-12-29" && b.Cost >= 1.5 {
			found = true
		}
	}
	if !found {
		t.Fatal("UTC Monday bucket missing")
	}
	if n, err := store.PruneEvents(ctx, 90); err != nil || n != 1 {
		t.Fatalf("prune %d %v", n, err)
	}
	after, err := store.UsageReport(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("accounting changed after event pruning")
	}
	other, err := store.UsageReport(ctx, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Runs) != 0 || other.Total != 0 {
		t.Fatal("cross-project usage leak")
	}
}
