package database

import (
	"context"
	"testing"
	"time"
)

func TestModelCacheRoundTrip(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()

	payload, fetchedAt, err := store.GetModelCache(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil || !fetchedAt.IsZero() {
		t.Fatalf("empty cache = %q %v, want no-cache signal", payload, fetchedAt)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := store.SetModelCache(ctx, []byte(`[{"id":"a"}]`), now); err != nil {
		t.Fatal(err)
	}
	gotPayload, gotFetchedAt, err := store.GetModelCache(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotPayload) != `[{"id":"a"}]` {
		t.Fatalf("payload=%q", gotPayload)
	}
	if !gotFetchedAt.Equal(now) {
		t.Fatalf("fetchedAt=%v want %v", gotFetchedAt, now)
	}
}

func TestModelCacheUpsertOverwrites(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()

	first := time.Now().UTC().Truncate(time.Second)
	if err := store.SetModelCache(ctx, []byte(`[{"id":"a"}]`), first); err != nil {
		t.Fatal(err)
	}
	second := first.Add(time.Hour)
	if err := store.SetModelCache(ctx, []byte(`[{"id":"b"}]`), second); err != nil {
		t.Fatal(err)
	}

	payload, fetchedAt, err := store.GetModelCache(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `[{"id":"b"}]` {
		t.Fatalf("payload=%q, want overwritten value", payload)
	}
	if !fetchedAt.Equal(second) {
		t.Fatalf("fetchedAt=%v want %v", fetchedAt, second)
	}

	var count int
	if err := store.db.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM openrouter_model_cache").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("row count=%d, want single-row cache", count)
	}
}
