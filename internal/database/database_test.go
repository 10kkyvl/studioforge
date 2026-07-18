package database

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func testDB(t *testing.T) (*DB, *Store) {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "studioforge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, NewStore(db)
}
func TestMigrationsAndPragmas(t *testing.T) {
	db, _ := testDB(t)
	ctx := context.Background()
	if err := db.Integrity(ctx); err != nil {
		t.Fatal(err)
	}
	if got := db.JournalMode(ctx); got != "wal" {
		t.Fatalf("journal mode = %q", got)
	}
	var timeout, foreign int
	if err := db.SQL.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRow("PRAGMA foreign_keys").Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if timeout != 5000 || foreign != 1 {
		t.Fatalf("pragmas timeout=%d foreign=%d", timeout, foreign)
	}
	required := []string{"schema_migrations", "projects", "project_agents", "tasks", "runs", "run_events", "decisions", "studio_sessions", "resource_leases", "assets", "budgets", "usage_records"}
	for _, table := range required {
		var count int
		if err := db.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
}
func TestDemoIsolationAndRecovery(t *testing.T) {
	db, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	projects, err := store.ListProjects(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 3 {
		t.Fatalf("projects=%d", len(projects))
	}
	agents, err := store.ListAgents(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 {
		t.Fatalf("agents=%d", len(agents))
	}
	for _, agent := range agents {
		if agent.ProjectID != "demo-obby" {
			t.Fatalf("cross-project agent: %+v", agent)
		}
	}
	tasks, err := store.ListTasks(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	for _, task := range tasks {
		if task.Dependencies == nil {
			t.Fatalf("task %s serialized dependencies as null", task.ID)
		}
	}
	_, err = db.SQL.Exec("UPDATE runs SET status='running' WHERE id='demo-obby-history'")
	if err != nil {
		t.Fatal(err)
	}
	count, err := store.RecoverInterrupted(ctx)
	if err != nil || count != 1 {
		t.Fatalf("recovery count=%d err=%v", count, err)
	}
	run, err := store.Run(ctx, "demo-obby-history")
	if err != nil || run.Status != "interrupted" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
}

func TestTaglessProjectUsesEmptyTagCollection(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	created, err := store.CreateProject(ctx, models.Project{
		Name:        "Tagless project",
		Path:        filepath.Join(t.TempDir(), "tagless-project"),
		Fingerprint: "tagless-project-fingerprint",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Tags == nil {
		t.Fatal("created project serialized tags as null")
	}

	projects, err := store.ListProjects(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects=%d", len(projects))
	}
	if projects[0].Tags == nil {
		t.Fatal("listed project serialized tags as null")
	}
}

func TestEnsureDefaultAgentCreatesOnlyOnce(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, err := store.CreateProject(ctx, models.Project{Name: "Agent project", Path: filepath.Join(t.TempDir(), "agent-project"), Fingerprint: "agent-project"})
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := store.EnsureDefaultAgent(ctx, project.ID, "codex", "default", "high")
	if err != nil || !created {
		t.Fatalf("first=%+v created=%v err=%v", first, created, err)
	}
	second, created, err := store.EnsureDefaultAgent(ctx, project.ID, "claude", "other", "low")
	if err != nil || created || second.ID != first.ID || second.Provider != "codex" || second.Effort != "high" {
		t.Fatalf("second=%+v created=%v err=%v", second, created, err)
	}
}

func TestEventOrderingAndConcurrentWrites(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	const writers = 8
	const each = 40
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				_, err := store.AppendEvents(ctx, []models.RunEvent{{ProjectID: "demo-obby", RunID: "demo-obby-history", AgentID: "demo-obby-orch", Type: "stress", Payload: map[string]int{"writer": writer, "index": i}}})
				if err != nil {
					errs <- err
					return
				}
			}
		}(writer)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	events, err := store.EventsAfter(ctx, 0, "demo-obby", "demo-obby-history", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != writers*each+1 {
		t.Fatalf("events=%d", len(events))
	}
	for i := 1; i < len(events); i++ {
		if events[i].ID <= events[i-1].ID {
			t.Fatalf("non-monotonic IDs %d then %d", events[i-1].ID, events[i].ID)
		}
	}
}
func TestBackupRestore(t *testing.T) {
	db, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "backup.db")
	if err := db.Backup(ctx, target); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.Integrity(ctx); err != nil {
		t.Fatal(err)
	}
	projects, err := NewStore(restored).ListProjects(ctx, true)
	if err != nil || len(projects) != 3 {
		t.Fatalf("restored projects=%d err=%v", len(projects), err)
	}
}
func TestIdempotentRunCreation(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	input := models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced"}
	first, created, err := store.CreateRun(ctx, input, "request-1")
	if err != nil || !created {
		t.Fatalf("first: %v %v", created, err)
	}
	second, created, err := store.CreateRun(ctx, input, "request-1")
	if err != nil || created || first.ID != second.ID {
		t.Fatalf("second=%+v created=%v err=%v", second, created, err)
	}
}
func TestBudgetEnforcement(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	allowed, limit, used, err := store.BudgetAllowed(ctx, "demo-obby", 30)
	if err != nil {
		t.Fatal(err)
	}
	if allowed || limit != 25 || used <= 0 {
		t.Fatalf("allowed=%v limit=%f used=%f", allowed, limit, used)
	}
	allowed, _, _, err = store.BudgetAllowed(ctx, "demo-obby", 1)
	if err != nil || !allowed {
		t.Fatalf("small request allowed=%v err=%v", allowed, err)
	}
}
