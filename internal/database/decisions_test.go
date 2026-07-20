package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func seedDecisionFixture(t *testing.T, store *Store) (models.Project, models.Run) {
	t.Helper()
	ctx := context.Background()
	project, err := store.CreateProject(ctx, models.Project{Name: "Decision project", Path: filepath.Join(t.TempDir(), "decision-project"), Fingerprint: "decision-project"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.CreateAgent(ctx, models.Agent{ProjectID: project.ID, Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, models.Run{ProjectID: project.ID, AgentID: agent.ID, Provider: "claude", ModelAlias: "balanced", Status: "completed", Phase: "verified", Validation: "failed"}, "")
	if err != nil {
		t.Fatal(err)
	}
	return project, run
}

func TestCreateDecisionAndListPending(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, run := seedDecisionFixture(t, store)

	decision, err := store.CreateDecision(ctx, models.Decision{ProjectID: project.ID, RunID: run.ID, Kind: "correction_run", Summary: "Correction run proposed", Detail: "limit reached", Payload: `{"runId":"x"}`})
	if err != nil {
		t.Fatal(err)
	}
	if decision.ID == "" || decision.Status != "pending" {
		t.Fatalf("decision=%+v, want a pending id assigned", decision)
	}

	pending, err := store.ListDecisions(ctx, "pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != decision.ID {
		t.Fatalf("pending=%+v, want the created decision", pending)
	}
	if pending[0].Payload != `{"runId":"x"}` {
		t.Errorf("payload=%q, want it round-tripped exactly", pending[0].Payload)
	}
}

func TestListDecisionsWithNoStatusFilterReturnsEvery(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, run := seedDecisionFixture(t, store)
	first, err := store.CreateDecision(ctx, models.Decision{ProjectID: project.ID, RunID: run.ID, Kind: "correction_run", Summary: "a", Payload: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveDecision(ctx, first.ID, "approved"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateDecision(ctx, models.Decision{ProjectID: project.ID, RunID: run.ID, Kind: "correction_run", Summary: "b", Payload: "{}"}); err != nil {
		t.Fatal(err)
	}

	all, err := store.ListDecisions(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all=%d, want 2 regardless of status", len(all))
	}
	pending, err := store.ListDecisions(ctx, "pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Summary != "b" {
		t.Fatalf("pending=%+v, want only the unresolved one", pending)
	}
}

func TestResolveDecisionSetsStatusAndResolvedAt(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, run := seedDecisionFixture(t, store)
	decision, err := store.CreateDecision(ctx, models.Decision{ProjectID: project.ID, RunID: run.ID, Kind: "correction_run", Summary: "a", Payload: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveDecision(ctx, decision.ID, "denied"); err != nil {
		t.Fatal(err)
	}
	all, err := store.ListDecisions(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Status != "denied" || all[0].ResolvedAt == nil {
		t.Fatalf("decision=%+v, want status=denied with resolvedAt set", all[0])
	}
}

func TestResolveDecisionTwiceErrors(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, run := seedDecisionFixture(t, store)
	decision, err := store.CreateDecision(ctx, models.Decision{ProjectID: project.ID, RunID: run.ID, Kind: "correction_run", Summary: "a", Payload: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveDecision(ctx, decision.ID, "approved"); err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveDecision(ctx, decision.ID, "approved"); err == nil {
		t.Fatal("resolving an already-resolved decision must error, not silently succeed twice")
	}
}

func TestResolveUnknownDecisionErrors(t *testing.T) {
	_, store := testDB(t)
	if err := store.ResolveDecision(context.Background(), "does-not-exist", "approved"); err == nil {
		t.Fatal("resolving a decision that does not exist must error")
	}
}
