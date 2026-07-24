package database

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

// Two submissions carrying the same Idempotency-Key both miss the lookup that
// precedes the insert. Before the insert itself handled the conflict, the
// loser surfaced a raw UNIQUE constraint violation as a failed request instead
// of returning the run the winner had just created.
func TestConcurrentCreateRunWithSameIdempotencyKeyReturnsOneRun(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "idempotency.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := NewStore(db)

	project, err := store.CreateProject(ctx, models.Project{ID: NewID(), Name: "race", Path: t.TempDir(), Fingerprint: NewID()})
	if err != nil {
		t.Fatal(err)
	}
	agent, _, err := store.EnsureDefaultAgent(ctx, project.ID, "mock", "default", "medium")
	if err != nil {
		t.Fatal(err)
	}

	const attempts = 8
	var wg sync.WaitGroup
	ids := make([]string, attempts)
	created := make([]bool, attempts)
	errs := make([]error, attempts)
	start := make(chan struct{})
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			run, wasCreated, err := store.CreateRun(ctx, models.Run{ProjectID: project.ID, AgentID: agent.ID, Provider: "mock", ModelAlias: "default"}, "same-key")
			ids[i], created[i], errs[i] = run.ID, wasCreated, err
		}()
	}
	close(start)
	wg.Wait()

	creations := 0
	for i := range attempts {
		if errs[i] != nil {
			t.Fatalf("attempt %d failed instead of returning the existing run: %v", i, errs[i])
		}
		if ids[i] == "" {
			t.Fatalf("attempt %d returned no run", i)
		}
		if ids[i] != ids[0] {
			t.Fatalf("attempt %d returned run %q, want the same run as attempt 0 (%q)", i, ids[i], ids[0])
		}
		if created[i] {
			creations++
		}
	}
	if creations != 1 {
		t.Fatalf("exactly one attempt should report a creation, got %d", creations)
	}

	runs, err := store.ListRuns(ctx, project.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("want exactly one persisted run, got %d", len(runs))
	}
}
