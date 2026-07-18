package database

import (
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func TestCreateRunPersistsThreadAndPrompt(t *testing.T) {
	store, ctx := newThreadStore(t)
	thread, err := store.EnsureDefaultThread(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	created, ok, err := store.CreateRun(ctx, models.Run{
		ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced",
		ThreadID: thread.ID, PromptSnapshot: "Build me a lobby",
	}, "")
	if err != nil || !ok {
		t.Fatalf("create run: %v ok=%v", err, ok)
	}
	got, err := store.Run(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ThreadID != thread.ID {
		t.Errorf("thread_id not persisted: %q", got.ThreadID)
	}
	if got.PromptSnapshot != "Build me a lobby" {
		t.Errorf("prompt_snapshot not persisted: %q", got.PromptSnapshot)
	}
}

func TestSetRunUsagePersistsTokens(t *testing.T) {
	cases := []struct {
		name   string
		tokens models.TokenUsage
	}{
		{name: "a full report survives the round trip", tokens: models.TokenUsage{InputTokens: 1200, OutputTokens: 680, CacheReadTokens: 44000, CacheCreationTokens: 3100}},
		{name: "a provider that reports no cache leaves those counters at zero", tokens: models.TokenUsage{InputTokens: 90, OutputTokens: 10}},
		{name: "a run that reported nothing stays at zero", tokens: models.TokenUsage{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := newThreadStore(t)
			created, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced"}, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SetRunUsage(ctx, created.ID, "sess-1", 0.42, tc.tokens); err != nil {
				t.Fatal(err)
			}
			got, err := store.Run(ctx, created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.TokenUsage != tc.tokens {
				t.Errorf("tokens=%+v want %+v", got.TokenUsage, tc.tokens)
			}
			if got.Cost != 0.42 || got.ProviderSession != "sess-1" {
				t.Errorf("tokens must land with the session and cost, got cost=%v session=%q", got.Cost, got.ProviderSession)
			}
			// The list view reads its own SELECT, which has to expose the same
			// columns or the run table would show every run at zero.
			list, err := store.ListRuns(ctx, "demo-obby", 10)
			if err != nil {
				t.Fatal(err)
			}
			for _, run := range list {
				if run.ID == created.ID && run.TokenUsage != tc.tokens {
					t.Errorf("listed tokens=%+v want %+v", run.TokenUsage, tc.tokens)
				}
			}
		})
	}
}

// SetRunUsage is the only place a run's spend reaches usage_records, and
// usage_records — not runs — is what the budget gate sums over. If this write
// went missing, BudgetAllowed would silently stop firing no matter how much a
// run spent.
func TestSetRunUsageWritesUsageRecord(t *testing.T) {
	store, ctx := newThreadStore(t)
	created, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, usedBefore, err := store.BudgetAllowed(ctx, "demo-obby", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetRunUsage(ctx, created.ID, "sess-budget", 1.5, models.TokenUsage{InputTokens: 400, OutputTokens: 100, CacheReadTokens: 20}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_records WHERE run_id=?", created.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("usage_records rows for run=%d want 1", count)
	}
	var projectID, agentID, provider, modelAlias string
	var inputTokens, outputTokens int
	var cost float64
	err = store.db.SQL.QueryRowContext(ctx, "SELECT project_id,agent_id,provider,model_alias,input_tokens,output_tokens,cost FROM usage_records WHERE run_id=?", created.ID).
		Scan(&projectID, &agentID, &provider, &modelAlias, &inputTokens, &outputTokens, &cost)
	if err != nil {
		t.Fatal(err)
	}
	if projectID != "demo-obby" || agentID != "demo-obby-orch" || provider != "mock" || modelAlias != "balanced" || inputTokens != 400 || outputTokens != 100 || cost != 1.5 {
		t.Errorf("usage_records row = project=%q agent=%q provider=%q model=%q in=%d out=%d cost=%v",
			projectID, agentID, provider, modelAlias, inputTokens, outputTokens, cost)
	}
	_, _, usedAfter, err := store.BudgetAllowed(ctx, "demo-obby", 0)
	if err != nil {
		t.Fatal(err)
	}
	if usedAfter != usedBefore+1.5 {
		t.Errorf("BudgetAllowed used=%v want %v (before=%v)", usedAfter, usedBefore+1.5, usedBefore)
	}
}

// A run that never spent anything (e.g. it failed before the provider
// reported usage) must not fabricate a usage_records row.
func TestSetRunUsageZeroCostStillRecordsRow(t *testing.T) {
	store, ctx := newThreadStore(t)
	created, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetRunUsage(ctx, created.ID, "sess-zero", 0, models.TokenUsage{}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_records WHERE run_id=?", created.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("usage_records rows for a zero-cost run=%d want 1 (cost still lands, just at zero)", count)
	}
}

// SetRunUsage on an id that does not exist must fail rather than silently
// write an orphan usage_records row with an empty project/agent.
func TestSetRunUsageUnknownRunFails(t *testing.T) {
	store, ctx := newThreadStore(t)
	if err := store.SetRunUsage(ctx, "missing-run", "sess", 1, models.TokenUsage{}); err == nil {
		t.Fatal("expected an error for an unknown run id")
	}
	var count int
	if err := store.db.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_records WHERE run_id='missing-run'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("usage_records rows for a failed write=%d want 0", count)
	}
}
