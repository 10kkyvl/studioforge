package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

func TestLegacyCodexRunReadsBackAndSerializes(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()
	created, _, err := a.store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "codex", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	loaded, err := a.store.Run(ctx, created.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if loaded.Provider != "codex" {
		t.Fatalf("provider=%q, want codex", loaded.Provider)
	}
	if _, err := json.Marshal(loaded); err != nil {
		t.Fatalf("legacy codex run did not serialize: %v", err)
	}
}

func TestLegacyCodexProviderIsNotConfigured(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()
	if _, configured := a.scheduler.Diagnose(ctx, "codex"); configured {
		t.Fatal("removed codex provider must report configured=false")
	}
	if _, _, err := a.scheduler.Submit(ctx, scheduler.Job{Provider: "codex"}); err == nil {
		t.Fatal("submitting a run against the removed codex provider must fail")
	}
}
