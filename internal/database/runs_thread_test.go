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
