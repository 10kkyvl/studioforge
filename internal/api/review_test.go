package api

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/gitops"
	"github.com/10kkyvl/studioforge/internal/models"
)

func gitCapture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

type reviewFixture struct {
	repo    string
	project models.Project
	run     models.Run
	review  models.RunReview
}

func newReviewFixture(t *testing.T, a *testAPI, expiresAt time.Time) reviewFixture {
	t.Helper()
	ctx := context.Background()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "StudioForge Test")
	runGit(t, repo, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(repo, "a.lua"), []byte("a1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "b.lua"), []byte("b1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "checkpoint")
	checkpoint := gitCapture(t, repo, "rev-parse", "HEAD")

	project, err := a.store.CreateProject(ctx, models.Project{Name: "review-fixture", Path: repo, Fingerprint: "review-fixture-" + repo})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := a.store.CreateAgent(ctx, models.Agent{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := a.store.CreateRun(ctx, models.Run{ProjectID: project.ID, AgentID: agent.ID, Provider: agent.Provider, ModelAlias: agent.ModelAlias, Status: "waiting_decision", Phase: "review"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.CreateCheckpoint(ctx, models.Checkpoint{ProjectID: project.ID, RunID: run.ID, CommitHash: checkpoint, Branch: "master", Label: "before"}); err != nil {
		t.Fatal(err)
	}
	review, err := a.store.CreateReview(ctx, models.RunReview{RunID: run.ID, ProjectID: project.ID, CheckpointHash: checkpoint, ExpiresAt: expiresAt})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo, "a.lua"), []byte("a2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "b.lua"), []byte("b2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return reviewFixture{repo: repo, project: project, run: run, review: review}
}

func newReviewTestAPI(t *testing.T) *testAPI {
	t.Helper()
	a := newTestAPI(t)
	a.server.git = &realGit{client: gitops.New()}
	return a
}

func TestReviewApplyKeepsEverythingAndCompletesTheRun(t *testing.T) {
	a := newReviewTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fx := newReviewFixture(t, a, time.Now().Add(time.Hour))

	rec := postJSON(t, a, cookie, "/api/v1/runs/"+fx.run.ID+"/review", map[string]any{"action": "apply"})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status        string `json:"status"`
		SafetyCommit  string `json:"safetyCommit"`
		RevertedFiles int    `json:"revertedFiles"`
		RevertedHunks int    `json:"revertedHunks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "applied" || body.SafetyCommit != "" || body.RevertedFiles != 0 || body.RevertedHunks != 0 {
		t.Fatalf("body=%+v", body)
	}
	gotA, _ := os.ReadFile(filepath.Join(fx.repo, "a.lua"))
	gotB, _ := os.ReadFile(filepath.Join(fx.repo, "b.lua"))
	if string(gotA) != "a2\n" || string(gotB) != "b2\n" {
		t.Fatalf("apply must not touch the working tree: a=%q b=%q", gotA, gotB)
	}
	run, err := a.store.Run(context.Background(), fx.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" {
		t.Fatalf("run.Status=%q, want completed", run.Status)
	}
	review, ok, err := a.store.Review(context.Background(), fx.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || review.Status != "applied" {
		t.Fatalf("review=%+v ok=%v, want status=applied", review, ok)
	}
}

func TestReviewRejectRevertsEveryChangedFile(t *testing.T) {
	a := newReviewTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fx := newReviewFixture(t, a, time.Now().Add(time.Hour))

	rec := postJSON(t, a, cookie, "/api/v1/runs/"+fx.run.ID+"/review", map[string]any{"action": "reject"})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status        string `json:"status"`
		RevertedFiles int    `json:"revertedFiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "rejected" || body.RevertedFiles != 2 {
		t.Fatalf("body=%+v", body)
	}
	gotA, _ := os.ReadFile(filepath.Join(fx.repo, "a.lua"))
	gotB, _ := os.ReadFile(filepath.Join(fx.repo, "b.lua"))
	if string(gotA) != "a1\n" || string(gotB) != "b1\n" {
		t.Fatalf("reject must revert every changed file: a=%q b=%q", gotA, gotB)
	}
	review, ok, err := a.store.Review(context.Background(), fx.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || review.Status != "rejected" {
		t.Fatalf("review=%+v ok=%v, want status=rejected", review, ok)
	}
}

func TestReviewApplySelectedKeepsOnlyTheSelectedFiles(t *testing.T) {
	a := newReviewTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fx := newReviewFixture(t, a, time.Now().Add(time.Hour))

	rec := postJSON(t, a, cookie, "/api/v1/runs/"+fx.run.ID+"/review", map[string]any{"action": "apply-selected", "files": []string{"a.lua"}})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status        string `json:"status"`
		RevertedFiles int    `json:"revertedFiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "partial" || body.RevertedFiles != 1 {
		t.Fatalf("body=%+v", body)
	}
	gotA, _ := os.ReadFile(filepath.Join(fx.repo, "a.lua"))
	gotB, _ := os.ReadFile(filepath.Join(fx.repo, "b.lua"))
	if string(gotA) != "a2\n" {
		t.Fatalf("a.lua=%q, want the kept file untouched at a2", gotA)
	}
	if string(gotB) != "b1\n" {
		t.Fatalf("b.lua=%q, want the unselected file reverted to b1", gotB)
	}
}

func TestReviewApplySelectedInvertsHunkSelection(t *testing.T) {
	a := newReviewTestAPI(t)
	cookie := bootstrapCookie(t, a)
	ctx := context.Background()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "StudioForge Test")
	runGit(t, repo, "config", "core.autocrlf", "false")
	file := filepath.Join(repo, "game.lua")
	original := "local a = 1\nlocal b = 2\nlocal c = 3\nlocal d = 4\nlocal e = 5\nlocal f = 6\nlocal g = 7\nlocal h = 8\nlocal i = 9\nlocal j = 10\n"
	if err := os.WriteFile(file, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "checkpoint")
	checkpoint := gitCapture(t, repo, "rev-parse", "HEAD")
	changed := "local a = 100\nlocal b = 2\nlocal c = 3\nlocal d = 4\nlocal e = 5\nlocal f = 6\nlocal g = 7\nlocal h = 8\nlocal i = 9\nlocal j = 200\n"
	if err := os.WriteFile(file, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}

	project, err := a.store.CreateProject(ctx, models.Project{Name: "review-hunks", Path: repo, Fingerprint: "review-hunks-" + repo})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := a.store.CreateAgent(ctx, models.Agent{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := a.store.CreateRun(ctx, models.Run{ProjectID: project.ID, AgentID: agent.ID, Provider: agent.Provider, ModelAlias: agent.ModelAlias, Status: "waiting_decision", Phase: "review"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.CreateReview(ctx, models.RunReview{RunID: run.ID, ProjectID: project.ID, CheckpointHash: checkpoint, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	rec := postJSON(t, a, cookie, "/api/v1/runs/"+run.ID+"/review", map[string]any{
		"action": "apply-selected",
		"hunks":  []map[string]any{{"path": "game.lua", "index": 1}},
	})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got, _ := os.ReadFile(file)
	lines := strings.Split(string(got), "\n")
	if lines[0] != "local a = 1" {
		t.Fatalf("hunk 0 should have been reverted (kept only hunk 1), got %q", lines[0])
	}
	if lines[9] != "local j = 200" {
		t.Fatalf("hunk 1 should have been kept, got %q", lines[9])
	}
}

func TestReviewRefusesAnUnknownPathInTheSelection(t *testing.T) {
	a := newReviewTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fx := newReviewFixture(t, a, time.Now().Add(time.Hour))

	rec := postJSON(t, a, cookie, "/api/v1/runs/"+fx.run.ID+"/review", map[string]any{"action": "apply-selected", "files": []string{"missing.lua"}})
	if rec.Code != 400 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_selection") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestReviewRefusesWhenAlreadyResolved(t *testing.T) {
	a := newReviewTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fx := newReviewFixture(t, a, time.Now().Add(time.Hour))

	first := postJSON(t, a, cookie, "/api/v1/runs/"+fx.run.ID+"/review", map[string]any{"action": "apply"})
	if first.Code != 200 {
		t.Fatalf("first resolve status=%d body=%s", first.Code, first.Body.String())
	}
	second := postJSON(t, a, cookie, "/api/v1/runs/"+fx.run.ID+"/review", map[string]any{"action": "apply"})
	if second.Code != 409 {
		t.Fatalf("second resolve status=%d body=%s", second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), "review_not_pending") {
		t.Fatalf("body=%s", second.Body.String())
	}
}

func TestReviewRefusesAfterExpiry(t *testing.T) {
	a := newReviewTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fx := newReviewFixture(t, a, time.Now().Add(-time.Hour))

	rec := postJSON(t, a, cookie, "/api/v1/runs/"+fx.run.ID+"/review", map[string]any{"action": "apply"})
	if rec.Code != 409 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "review_expired") {
		t.Fatalf("body=%s", rec.Body.String())
	}
	review, ok, err := a.store.Review(context.Background(), fx.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || review.Status != "expired" {
		t.Fatalf("review=%+v ok=%v, want status=expired", review, ok)
	}
}

func TestReviewResolvesWhileItsOwnReviewLeaseIsHeld(t *testing.T) {
	a := newReviewTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fx := newReviewFixture(t, a, time.Now().Add(time.Hour))

	handle, err := a.leases.Acquire(context.Background(), "review:"+fx.run.ID, []string{"project:" + fx.project.ID + ":write"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handle.Release)

	rec := postJSON(t, a, cookie, "/api/v1/runs/"+fx.run.ID+"/review", map[string]any{"action": "apply"})
	if rec.Code != 200 {
		t.Fatalf("resolving a review must not be blocked by the lease it holds itself: status=%d body=%s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if owner := a.leases.Snapshot()["project:"+fx.project.ID+":write"]; owner == "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the project write lease was not released after the review resolved")
}

func TestReviewTakesASafetyCheckpointBeforeReverting(t *testing.T) {
	a := newReviewTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fx := newReviewFixture(t, a, time.Now().Add(time.Hour))

	before := gitCapture(t, fx.repo, "rev-list", "--count", "HEAD")
	rec := postJSON(t, a, cookie, "/api/v1/runs/"+fx.run.ID+"/review", map[string]any{"action": "reject"})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		SafetyCommit string `json:"safetyCommit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.SafetyCommit == "" {
		t.Fatal("expected a non-empty safety commit hash")
	}
	after := gitCapture(t, fx.repo, "rev-list", "--count", "HEAD")
	if after == before {
		t.Fatalf("expected a new commit for the safety checkpoint: before=%s after=%s", before, after)
	}
	checkpoints, err := a.store.CheckpointsForProject(context.Background(), fx.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range checkpoints {
		if c.CommitHash == body.SafetyCommit {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the safety commit to be persisted as a checkpoint")
	}
}
