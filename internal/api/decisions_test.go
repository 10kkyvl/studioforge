package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

func createTestDecision(t *testing.T, a *testAPI, correction scheduler.Job) models.Decision {
	t.Helper()
	payload, err := json.Marshal(correction)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := a.store.CreateRun(context.Background(), models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "claude", ModelAlias: "balanced", Status: "completed", Phase: "verified", Validation: "failed"}, "")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := a.store.CreateDecision(context.Background(), models.Decision{ProjectID: "demo-obby", RunID: run.ID, Kind: "correction_run", Summary: "Correction run proposed", Detail: "still broken", Payload: string(payload)})
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func TestResolveDecisionApprovingSchedulesTheCorrection(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	decision := createTestDecision(t, a, scheduler.Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", MaxBudget: 1, Prompt: "fix it"})

	rec := postJSON(t, a, cookie, "/api/v1/decisions/"+decision.ID+"/resolve", map[string]any{"approve": true})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	all, err := a.store.ListDecisions(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Status != "approved" {
		t.Fatalf("decisions=%+v, want approved", all)
	}
	runs, err := a.store.ListRuns(context.Background(), "demo-obby", 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range runs {
		if r.PromptSnapshot == "fix it" {
			found = true
		}
	}
	if !found {
		t.Error("approving must actually submit the proposed correction run")
	}
}

func TestResolveDecisionDenyingNeverSchedulesARun(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	decision := createTestDecision(t, a, scheduler.Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", MaxBudget: 1, Prompt: "fix it"})

	before, err := a.store.ListRuns(context.Background(), "demo-obby", 50)
	if err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, a, cookie, "/api/v1/decisions/"+decision.ID+"/resolve", map[string]any{"approve": false})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	after, err := a.store.ListRuns(context.Background(), "demo-obby", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("runs=%d after deny, want %d unchanged (no run scheduled)", len(after), len(before))
	}
	all, err := a.store.ListDecisions(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if all[0].Status != "denied" {
		t.Fatalf("decision status=%q, want denied", all[0].Status)
	}
}

func TestResolveDecisionTwiceReturnsAnError(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	decision := createTestDecision(t, a, scheduler.Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", MaxBudget: 1, Prompt: "fix it"})

	if rec := postJSON(t, a, cookie, "/api/v1/decisions/"+decision.ID+"/resolve", map[string]any{"approve": false}); rec.Code != 200 {
		t.Fatalf("first resolve status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec := postJSON(t, a, cookie, "/api/v1/decisions/"+decision.ID+"/resolve", map[string]any{"approve": true})
	if rec.Code == 200 {
		t.Fatal("resolving an already-resolved decision a second time must fail, not silently schedule a run")
	}
}

func TestResolveUnknownDecisionReturns404(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postJSON(t, a, cookie, "/api/v1/decisions/does-not-exist/resolve", map[string]any{"approve": true})
	if rec.Code != 404 {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestSnapshotCarriesPendingDecisions(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	createTestDecision(t, a, scheduler.Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", Prompt: "fix it"})

	rec := getJSON(t, a, cookie, "/api/v1/snapshot")
	var body struct {
		Decisions []models.Decision `json:"decisions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Decisions) != 1 || body.Decisions[0].Status != "pending" {
		t.Fatalf("decisions=%+v, want one pending decision", body.Decisions)
	}
}

func TestSnapshotReturnsEmptyDecisionArray(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := getJSON(t, a, cookie, "/api/v1/snapshot")
	var body struct {
		Decisions []models.Decision `json:"decisions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Decisions == nil || len(body.Decisions) != 0 {
		t.Fatalf("decisions=%+v, want empty array", body.Decisions)
	}
}
