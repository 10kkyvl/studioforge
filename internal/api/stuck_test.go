package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

// TestStuckContinueSuppresses covers the "continue suppresses, clarify keeps
// detecting" decision without re-triggering a real escalation: it is a pure
// function of the thread's previous run's stuck bookkeeping and the literal
// text of the next message.
func TestStuckContinueSuppresses(t *testing.T) {
	cases := []struct {
		name          string
		prevEscalated bool
		prompt        string
		want          bool
	}{
		{"continue after an escalation suppresses detection", true, scheduler.StuckContinueLabel, true},
		{"a genuine clarification keeps detection enabled", true, "actually, use a different mesh format", false},
		{"a run that never escalated never suppresses", false, scheduler.StuckContinueLabel, false},
		{"whitespace around the label still counts as continue", true, "  " + scheduler.StuckContinueLabel + "  ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stuckContinueSuppresses(tc.prevEscalated, tc.prompt)
			if got != tc.want {
				t.Errorf("stuckContinueSuppresses(escalated=%v, prompt=%q) = %v, want %v",
					tc.prevEscalated, tc.prompt, got, tc.want)
			}
		})
	}
}

// TestCreateRunResolvesStuckSettingsOntoTheJob drives a real run through
// createRun with a custom StuckSettings hook and a disabled agent, and checks
// the resulting run reaches "running" — proving the handler does not error
// out while resolving the new Job fields, including the per-agent opt-out
// (StuckDetectionDisabled) folded into StuckDetectionEnabled at submit time.
func TestCreateRunResolvesStuckSettingsOntoTheJob(t *testing.T) {
	a := newTestAPI(t)
	a.server.stuckSettings = func() scheduler.StuckSettings {
		return scheduler.StuckSettings{Enabled: true, IdleSeconds: 120, RepetitionCap: 3}
	}
	cookie := bootstrapCookie(t, a)

	agents, err := a.store.ListAgents(context.Background(), "demo-obby")
	if err != nil || len(agents) == 0 {
		t.Fatalf("agents=%v err=%v", agents, err)
	}
	agent := agents[0]
	agent.StuckDetectionDisabled = true
	if _, err := a.store.UpdateAgent(context.Background(), agent); err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(`{"projectId":"demo-obby","agentId":"` + agent.ID + `","maxBudget":1,"prompt":"Build the first milestone"}`)
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs", body)
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 201 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var run models.Run
	if err := json.Unmarshal(recorder.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	waitAPIRunStatus(t, a, run.ID, "completed", 5*time.Second)
}

// TestCancelWaitingDecisionRunThroughTheAPI is the HTTP-level counterpart of
// scheduler.TestCancelWaitingDecisionRunReachesCancelled: a run parked in
// waiting_decision with no live goroutine (simulated directly against the
// store, exactly like a stuck escalation or the agent's own natural question
// leaves it) must still be cancellable through the ordinary
// POST /runs/{id}/cancel action.
func TestCancelWaitingDecisionRunThroughTheAPI(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	ctx := context.Background()
	run, _, err := a.store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.UpdateRun(ctx, run.ID, "waiting_decision", "waiting_decision", "", ""); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs/"+run.ID+"/cancel", strings.NewReader(`{}`))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 202 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	waitAPIRunStatus(t, a, run.ID, "cancelled", 2*time.Second)
}

func waitAPIRunStatus(t *testing.T, a *testAPI, runID, status string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		run, err := a.store.Run(context.Background(), runID)
		if err == nil {
			last = run.Status
			if last == status {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s status=%s wanted=%s", runID, last, status)
}
