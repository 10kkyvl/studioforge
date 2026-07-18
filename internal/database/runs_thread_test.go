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
