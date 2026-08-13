package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/gitops"
	"github.com/10kkyvl/studioforge/internal/gitops/diffparse"
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

const sampleUnifiedDiff = `diff --git a/src/Main.luau b/src/Main.luau
index abc1234..def5678 100644
--- a/src/Main.luau
+++ b/src/Main.luau
@@ -1,1 +1,2 @@
-local x = 1
+local x = 2
+local y = 3`

// ?format=structured must return the #8 model (stats/files) instead of the
// raw diff string, while note/checkpoint/studioDirectEdits keep behaving
// exactly as the raw response does.
func TestRunDiffStructuredReturnsStatsAndFiles(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	run := createDiffTestRun(t, a)
	checkpoint := models.Checkpoint{ProjectID: "demo-obby", RunID: run.ID, CommitHash: "abc123", Branch: "main", Label: "before run"}
	if err := a.store.CreateCheckpoint(context.Background(), checkpoint); err != nil {
		t.Fatal(err)
	}
	a.server.git = &fakeGitOps{diffCommitOut: sampleUnifiedDiff}

	rec := getJSON(t, a, cookie, "/api/v1/runs/"+run.ID+"/diff?format=structured")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Stats             diffparse.DiffStats  `json:"stats"`
		Files             []diffparse.DiffFile `json:"files"`
		Diff              *string              `json:"diff"`
		Note              *string              `json:"note"`
		StudioDirectEdits bool                 `json:"studioDirectEdits"`
		Checkpoint        *struct {
			CommitHash string    `json:"commitHash"`
			Branch     string    `json:"branch"`
			Label      string    `json:"label"`
			CreatedAt  time.Time `json:"createdAt"`
		} `json:"checkpoint"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Diff != nil {
		t.Fatalf("structured response must not carry a raw diff field, got %v", *body.Diff)
	}
	if body.Note != nil {
		t.Fatalf("note should be absent on success, got %v", *body.Note)
	}
	if body.Stats.FilesChanged != 1 || body.Stats.Additions != 2 || body.Stats.Deletions != 1 {
		t.Fatalf("stats=%+v", body.Stats)
	}
	if len(body.Files) != 1 || body.Files[0].Path != "src/Main.luau" {
		t.Fatalf("files=%+v", body.Files)
	}
	if body.Checkpoint == nil || body.Checkpoint.CommitHash != "abc123" {
		t.Fatalf("checkpoint=%+v", body.Checkpoint)
	}
}

// Without ?format=structured the response must stay exactly what it always
// was: a "diff" string, no "stats"/"files" keys.
func TestRunDiffWithoutFormatParamStaysRaw(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	run := createDiffTestRun(t, a)
	a.server.git = &fakeGitOps{diffCommitOut: sampleUnifiedDiff}

	rec := getJSON(t, a, cookie, "/api/v1/runs/"+run.ID+"/diff")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["diff"]; !ok {
		t.Fatalf("expected a raw diff field, body=%s", rec.Body.String())
	}
	if _, ok := body["stats"]; ok {
		t.Fatalf("raw response must not carry stats, body=%s", rec.Body.String())
	}
	if _, ok := body["files"]; ok {
		t.Fatalf("raw response must not carry files, body=%s", rec.Body.String())
	}
}

// The structured format's "not available" state must mirror the raw
// format's: a note explaining why, plus zero stats and an empty files array
// rather than a null one.
func TestRunDiffStructuredWhenGitUnavailable(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	run := createDiffTestRun(t, a)
	a.server.git = nil

	rec := getJSON(t, a, cookie, "/api/v1/runs/"+run.ID+"/diff?format=structured")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Stats diffparse.DiffStats  `json:"stats"`
		Files []diffparse.DiffFile `json:"files"`
		Note  string               `json:"note"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Note != "Diffing is not available" {
		t.Fatalf("note=%q", body.Note)
	}
	if body.Stats != (diffparse.DiffStats{}) {
		t.Fatalf("stats=%+v", body.Stats)
	}
	if body.Files == nil || len(body.Files) != 0 {
		t.Fatalf("files=%+v", body.Files)
	}
}

func TestProjectCheckpointsListsNewestFirstNeverNull(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	run := createDiffTestRun(t, a)
	older := models.Checkpoint{ProjectID: "demo-obby", RunID: run.ID, CommitHash: "older-hash", Branch: "main", Label: "older", CreatedAt: time.Now().UTC().Add(-time.Hour)}
	newer := models.Checkpoint{ProjectID: "demo-obby", RunID: run.ID, CommitHash: "newer-hash", Branch: "main", Label: "newer", CreatedAt: time.Now().UTC()}
	if err := a.store.CreateCheckpoint(context.Background(), older); err != nil {
		t.Fatal(err)
	}
	if err := a.store.CreateCheckpoint(context.Background(), newer); err != nil {
		t.Fatal(err)
	}

	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/checkpoints")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Checkpoints []struct {
			CommitHash string `json:"commitHash"`
			Branch     string `json:"branch"`
			Label      string `json:"label"`
			RunID      string `json:"runId"`
		} `json:"checkpoints"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Checkpoints) != 2 {
		t.Fatalf("checkpoints=%+v", body.Checkpoints)
	}
	if body.Checkpoints[0].CommitHash != "newer-hash" || body.Checkpoints[1].CommitHash != "older-hash" {
		t.Fatalf("expected newest-first order, got %+v", body.Checkpoints)
	}
	if body.Checkpoints[0].RunID != run.ID {
		t.Fatalf("runId=%q", body.Checkpoints[0].RunID)
	}
}

func TestProjectCheckpointsEmptyReturnsEmptyArrayNotNull(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)

	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/checkpoints")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	checkpoints, ok := body["checkpoints"].([]any)
	if !ok {
		t.Fatalf("checkpoints missing or wrong type, body=%s", rec.Body.String())
	}
	if len(checkpoints) != 0 {
		t.Fatalf("checkpoints=%v", checkpoints)
	}
}

func TestProjectDiffMissingFromReturns400(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)

	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/diff")
	if rec.Code != 400 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "missing_from" {
		t.Fatalf("code=%q", body.Error.Code)
	}
}

func TestProjectDiffUnknownCheckpointReturns404(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.git = &fakeGitOps{}

	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/diff?from=does-not-exist")
	if rec.Code != 404 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "unknown_checkpoint" {
		t.Fatalf("code=%q", body.Error.Code)
	}
}

// A valid known checkpoint as "from" with no "to" must default "to" to HEAD
// and return the #8 structured model built from whatever DiffRange returns.
func TestProjectDiffDefaultsToHEADAndReturnsStructuredModel(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	run := createDiffTestRun(t, a)
	checkpoint := models.Checkpoint{ProjectID: "demo-obby", RunID: run.ID, CommitHash: "known-hash", Branch: "main", Label: "checkpoint"}
	if err := a.store.CreateCheckpoint(context.Background(), checkpoint); err != nil {
		t.Fatal(err)
	}
	fake := &fakeGitOps{diffRangeOut: sampleUnifiedDiff}
	a.server.git = fake

	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/diff?from=known-hash")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotFrom != "known-hash" || fake.gotTo != "HEAD" {
		t.Fatalf("from=%q to=%q, want known-hash/HEAD", fake.gotFrom, fake.gotTo)
	}
	var body struct {
		Stats diffparse.DiffStats  `json:"stats"`
		Files []diffparse.DiffFile `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Stats.FilesChanged != 1 || len(body.Files) != 1 {
		t.Fatalf("body=%+v", body)
	}
}

func TestProjectDiffNotAncestorReturns400InvalidRange(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	run := createDiffTestRun(t, a)
	checkpoint := models.Checkpoint{ProjectID: "demo-obby", RunID: run.ID, CommitHash: "known-hash", Branch: "main", Label: "checkpoint"}
	if err := a.store.CreateCheckpoint(context.Background(), checkpoint); err != nil {
		t.Fatal(err)
	}
	a.server.git = &fakeGitOps{diffRangeErr: gitops.ErrNotAncestor}

	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/diff?from=known-hash&to=HEAD")
	if rec.Code != 400 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "invalid_range" {
		t.Fatalf("code=%q", body.Error.Code)
	}
}

func TestProjectDiffGitUnavailableReturns200WithNote(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.git = nil

	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/diff?from=HEAD&to=HEAD")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Stats diffparse.DiffStats  `json:"stats"`
		Files []diffparse.DiffFile `json:"files"`
		Note  string               `json:"note"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Note != "Diffing is not available" {
		t.Fatalf("note=%q", body.Note)
	}
	if body.Files == nil || len(body.Files) != 0 {
		t.Fatalf("files=%+v", body.Files)
	}
}

// A project directory that is not a git repository at all must render the
// same empty "no changes" state as runDiff's own not-a-git-repo case: 200,
// no raw git error leaked, empty stats/files.
func TestProjectDiffNotAGitRepoReturns200WithNote(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.git = &realGit{client: gitops.New()}

	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/diff?from=HEAD&to=HEAD")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Stats diffparse.DiffStats  `json:"stats"`
		Files []diffparse.DiffFile `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Stats != (diffparse.DiffStats{}) || len(body.Files) != 0 {
		t.Fatalf("body=%+v", body)
	}
}
