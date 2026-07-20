package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/gitops"
	"github.com/10kkyvl/studioforge/internal/models"
)

type realGit struct{ client *gitops.Client }

func (g *realGit) DiffHead(ctx context.Context, path string) (string, error) {
	return g.client.DiffHead(ctx, path)
}
func (g *realGit) DiffCommit(ctx context.Context, path, commit string) (string, error) {
	return g.client.DiffCommit(ctx, path, commit)
}
func (g *realGit) Status(ctx context.Context, path string) (string, error) {
	return g.client.Status(ctx, path)
}
func (g *realGit) SafeRollback(ctx context.Context, path, target string) (string, error) {
	return g.client.SafeRollback(ctx, path, target)
}
func (g *realGit) Tag(ctx context.Context, path, name string) error {
	return g.client.Tag(ctx, path, name)
}

type fakeGitOps struct {
	statusOut      string
	diffCommitOut  string
	rollbackBranch string
	rollbackErr    error
	tagErr         error
	gotCommit      string
	gotTagName     string
}

func (f *fakeGitOps) DiffHead(ctx context.Context, path string) (string, error) { return "", nil }
func (f *fakeGitOps) DiffCommit(ctx context.Context, path, commit string) (string, error) {
	f.gotCommit = commit
	return f.diffCommitOut, nil
}
func (f *fakeGitOps) Status(ctx context.Context, path string) (string, error) {
	return f.statusOut, nil
}
func (f *fakeGitOps) SafeRollback(ctx context.Context, path, target string) (string, error) {
	f.gotCommit = target
	return f.rollbackBranch, f.rollbackErr
}
func (f *fakeGitOps) Tag(ctx context.Context, path, name string) error {
	f.gotTagName = name
	return f.tagErr
}

func TestGitStatusOnNonGitProjectDirectorySurfacesCleanly(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.git = &realGit{client: gitops.New()}
	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/git/status")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "" || body.Note == "" {
		t.Fatalf("non-git project must report an empty status and a note, got %+v", body)
	}
}

func TestGitStatusReturnsGitOutput(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.git = &fakeGitOps{statusOut: "## main"}
	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/git/status")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "## main" {
		t.Fatalf("status=%q", body.Status)
	}
}

func TestRollbackWithMissingCheckpointIsRefused(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.git = &fakeGitOps{rollbackBranch: "studioforge/rollback-x"}
	rec := postJSON(t, a, cookie, "/api/v1/runs/demo-obby-history/rollback", map[string]any{})
	if rec.Code != 400 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no_checkpoint") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestRollbackRefusedWhileProjectWriteLeaseIsHeld(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	if err := a.store.CreateCheckpoint(context.Background(), models.Checkpoint{ProjectID: "demo-obby", RunID: "demo-obby-history", CommitHash: "deadbeef", Branch: "main", Label: "before"}); err != nil {
		t.Fatal(err)
	}
	submitBody := strings.NewReader(`{"projectId":"demo-obby","agentId":"demo-obby-orch","maxBudget":1,"prompt":"Build the first milestone"}`)
	submitRequest := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs", submitBody)
	submitRequest.Header.Set("Origin", "http://127.0.0.1:1234")
	submitRequest.Header.Set("Content-Type", "application/json")
	submitRequest.AddCookie(cookie)
	submitRecorder := httptest.NewRecorder()
	a.handler.ServeHTTP(submitRecorder, submitRequest)
	if submitRecorder.Code != 201 {
		t.Fatalf("submit status=%d body=%s", submitRecorder.Code, submitRecorder.Body.String())
	}
	var run models.Run
	if err := json.Unmarshal(submitRecorder.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, a.store, run.ID, "running", 5*time.Second)

	rec := postJSON(t, a, cookie, "/api/v1/runs/demo-obby-history/rollback", map[string]any{})
	if rec.Code != 409 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "project_busy") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestRollbackRefusedWhenLeaseCheckUnavailable(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.git = &fakeGitOps{rollbackBranch: "studioforge/rollback-x"}
	if err := a.store.CreateCheckpoint(context.Background(), models.Checkpoint{ProjectID: "demo-obby", RunID: "demo-obby-history", CommitHash: "deadbeef", Branch: "main", Label: "before"}); err != nil {
		t.Fatal(err)
	}
	a.server.leases = nil
	rec := postJSON(t, a, cookie, "/api/v1/runs/demo-obby-history/rollback", map[string]any{})
	if rec.Code != 409 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "lease_check_unavailable") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestRollbackOnNonGitProjectDirectorySurfacesCleanly(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.git = &realGit{client: gitops.New()}
	if err := a.store.CreateCheckpoint(context.Background(), models.Checkpoint{ProjectID: "demo-obby", RunID: "demo-obby-history", CommitHash: "deadbeef", Branch: "main", Label: "before"}); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, a, cookie, "/api/v1/runs/demo-obby-history/rollback", map[string]any{})
	if rec.Code != 409 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "rollback_failed") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestRollbackSuccessReturnsBranchAndCommit(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeGitOps{rollbackBranch: "studioforge/rollback-20260720-000000"}
	a.server.git = fake
	if err := a.store.CreateCheckpoint(context.Background(), models.Checkpoint{ProjectID: "demo-obby", RunID: "demo-obby-history", CommitHash: "deadbeef", Branch: "main", Label: "before"}); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, a, cookie, "/api/v1/runs/demo-obby-history/rollback", map[string]any{})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Branch     string `json:"branch"`
		CommitHash string `json:"commitHash"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Branch != fake.rollbackBranch || body.CommitHash != "deadbeef" {
		t.Fatalf("body=%+v", body)
	}
	if fake.gotCommit != "deadbeef" {
		t.Fatalf("SafeRollback got commit=%q, want deadbeef", fake.gotCommit)
	}
}

func TestGitTagRequiresName(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.git = &fakeGitOps{}
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/git/tag", map[string]any{})
	if rec.Code != 400 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGitTagCreatesAnnotatedTag(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeGitOps{}
	a.server.git = fake
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/git/tag", map[string]any{"name": "milestone-1"})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotTagName != "milestone-1" {
		t.Fatalf("gotTagName=%q", fake.gotTagName)
	}
}
