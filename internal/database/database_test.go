package database

import (
	"context"
	"io/fs"
	"path/filepath"
	"sync"
	"testing"

	"github.com/10kkyvl/studioforge/internal/migrations"
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
	required := []string{"schema_migrations", "projects", "project_agents", "tasks", "runs", "run_events", "studio_sessions", "resource_leases", "assets", "budgets", "usage_records"}
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

// The project card reads a project's lifetime spend straight off
// ListProjects, so the SUM has to add across every run in the project.
func TestListProjectsAggregatesTokens(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetRunUsage(ctx, run.ID, "sess-1", 0.3, models.TokenUsage{InputTokens: 500, OutputTokens: 300, CacheReadTokens: 9000, CacheCreationTokens: 40}); err != nil {
		t.Fatal(err)
	}
	projects, err := store.ListProjects(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	var got models.Project
	found := false
	for _, p := range projects {
		if p.ID == "demo-obby" {
			got, found = p, true
		}
	}
	if !found {
		t.Fatal("demo-obby not found")
	}
	// The demo seed's history run carries a usage_records row with non-zero
	// tokens (see demo.go), but that row is not what this SUM reads — it sums
	// runs.input_tokens etc. directly, and the seeded run's own runs row keeps
	// those columns at their default of 0. So only the new run's tokens show up.
	want := models.TokenUsage{InputTokens: 500, OutputTokens: 300, CacheReadTokens: 9000, CacheCreationTokens: 40}
	if got.TokenUsage != want {
		t.Errorf("project tokens=%+v want %+v", got.TokenUsage, want)
	}
}

// The 005 migration replays pre-existing runs.cost/tokens into usage_records
// so budget history does not silently start over. Exercised directly here
// (rather than via a second Open, since applyMigrations only ever runs a
// migration once per database) to prove the SQL itself is idempotent and
// leaves the demo seed's own usage_records row alone.
func TestUsageBackfillMigrationBackfillsAndIsIdempotent(t *testing.T) {
	db, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// Stand in for a run written before usage_records existed on the live
	// path: insert it directly, bypassing SetRunUsage, so it has cost and
	// tokens on the runs row but no usage_records row of its own.
	legacyID := NewID()
	now := Now()
	_, err := db.SQL.ExecContext(ctx, `INSERT INTO runs(id,project_id,agent_id,provider,model_alias,status,phase,cost,input_tokens,output_tokens,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		legacyID, "demo-obby", "demo-obby-orch", "mock", "balanced", "completed", "verified", 2.5, 900, 300, now, now)
	if err != nil {
		t.Fatal(err)
	}
	body, err := fs.ReadFile(migrations.Files, "sql/005_usage_backfill.sql")
	if err != nil {
		t.Fatal(err)
	}
	// The migration ledger runs this exactly once in production; running it
	// twice here is how the test proves the NOT EXISTS guard actually holds.
	for i := 0; i < 2; i++ {
		if _, err := db.SQL.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	var count int
	if err := db.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_records WHERE run_id=?", legacyID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("usage_records rows for legacy run=%d want 1 (idempotent backfill)", count)
	}
	var cost float64
	var inputTokens, outputTokens int
	err = db.SQL.QueryRowContext(ctx, "SELECT cost,input_tokens,output_tokens FROM usage_records WHERE run_id=?", legacyID).
		Scan(&cost, &inputTokens, &outputTokens)
	if err != nil {
		t.Fatal(err)
	}
	if cost != 2.5 || inputTokens != 900 || outputTokens != 300 {
		t.Errorf("backfilled usage_records = cost=%v in=%d out=%d, want 2.5/900/300", cost, inputTokens, outputTokens)
	}
	// The demo seed already writes its own usage_records row for its history
	// run; the backfill running (twice, even) must not duplicate it.
	var demoCount int
	if err := db.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_records WHERE run_id='demo-obby-history'").Scan(&demoCount); err != nil {
		t.Fatal(err)
	}
	if demoCount != 1 {
		t.Fatalf("demo seed usage_records rows=%d want 1 (not duplicated by backfill)", demoCount)
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
