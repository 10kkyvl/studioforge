package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func createDiffTestRun(t *testing.T, a *testAPI) models.Run {
	t.Helper()
	run, _, err := a.store.CreateRun(context.Background(), models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "claude", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// A run that called a Studio MCP tool that changes the open place directly
// must carry that warning in its diff response even when git itself has
// nothing to show, since that is exactly when an operator is most likely to
// mistake "no changes" for "nothing happened".
func TestRunDiffCarriesStudioDirectEditsWhenFlagged(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	run := createDiffTestRun(t, a)
	if err := a.store.SetRunStudioDirectEdits(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	a.server.git = &fakeGitOps{diffCommitOut: "diff --git a/x b/x"}

	rec := getJSON(t, a, cookie, "/api/v1/runs/"+run.ID+"/diff")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		StudioDirectEdits bool `json:"studioDirectEdits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.StudioDirectEdits {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

// A run that never touched a Studio-mutating tool must report the flag as
// false rather than true, so the diff panel has no reason to warn about
// changes outside git.
func TestRunDiffReportsStudioDirectEditsFalseWhenNotFlagged(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	run := createDiffTestRun(t, a)
	a.server.git = &fakeGitOps{diffCommitOut: "diff --git a/x b/x"}

	rec := getJSON(t, a, cookie, "/api/v1/runs/"+run.ID+"/diff")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got, ok := body["studioDirectEdits"]; !ok || got != false {
		t.Fatalf("studioDirectEdits=%v (present=%v), want false", got, ok)
	}
}

// The operator needs the warning even when diffing itself is unavailable —
// that is precisely when they have the least other information about what
// this run actually did.
func TestRunDiffCarriesStudioDirectEditsWhenGitUnavailable(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	run := createDiffTestRun(t, a)
	if err := a.store.SetRunStudioDirectEdits(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	a.server.git = nil

	rec := getJSON(t, a, cookie, "/api/v1/runs/"+run.ID+"/diff")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		StudioDirectEdits bool `json:"studioDirectEdits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.StudioDirectEdits {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
