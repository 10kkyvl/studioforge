package api

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

type reviewGitStub struct{}

func (reviewGitStub) DiffHead(context.Context, string) (string, error)           { return "", nil }
func (reviewGitStub) DiffCommit(context.Context, string, string) (string, error) { return "", nil }
func (reviewGitStub) Status(context.Context, string) (string, error)             { return "", nil }
func (reviewGitStub) SafeRollback(context.Context, string, string) (string, error) {
	return "", nil
}
func (reviewGitStub) Tag(context.Context, string, string) error { return nil }

func reviewGitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

func reviewFixture(t *testing.T) (*testAPI, string, models.Run, string) {
	t.Helper()
	a := newEmptyTestAPI(t)
	root := t.TempDir()
	reviewGitRun(t, root, "init", "-q")
	reviewGitRun(t, root, "config", "core.autocrlf", "false")
	reviewGitRun(t, root, "config", "user.name", "StudioForge Review Test")
	reviewGitRun(t, root, "config", "user.email", "review@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\none\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reviewGitRun(t, root, "add", "tracked.txt")
	reviewGitRun(t, root, "commit", "-qm", "initial")
	project, err := a.store.CreateProject(context.Background(), models.Project{ID: "review-project", Name: "Review project", Path: root, Fingerprint: "review-project"})
	if err != nil {
		t.Fatal(err)
	}
	_ = project
	if _, err := a.store.CreateAgent(context.Background(), models.Agent{ID: "agent", ProjectID: project.ID, Name: "Review agent", Provider: "mock", ModelAlias: "balanced"}); err != nil {
		t.Fatal(err)
	}
	run, _, err := a.store.CreateRun(context.Background(), models.Run{ProjectID: "review-project", AgentID: "agent", Provider: "mock", ModelAlias: "balanced", Status: "running", Phase: "running"}, "")
	if err != nil {
		t.Fatal(err)
	}
	baseOut, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSpace(string(baseOut))
	a.server.git = reviewGitStub{}
	return a, root, run, base
}

func TestReviewAPIProposeAndApplySelectedHunk(t *testing.T) {
	a, root, run, base := reviewFixture(t)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proposal, err := a.server.proposeReview(context.Background(), scheduler.ReviewRequest{Job: &scheduler.Job{ProjectID: run.ProjectID, RunID: run.ID}, BaseCommit: base, Timeout: time.Minute})
	if err != nil || !proposal.Pending {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	decision, err := a.store.Decision(context.Background(), proposal.DecisionID)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ReviewFiles []models.ReviewFile `json:"reviewFiles"`
	}
	if err := json.Unmarshal([]byte(decision.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.ReviewFiles) != 1 || len(payload.ReviewFiles[0].Hunks) != 1 {
		t.Fatalf("structured review files=%+v", payload.ReviewFiles)
	}
	cookie := bootstrapCookie(t, a)
	rec := postJSON(t, a, cookie, "/api/v1/decisions/"+decision.ID+"/resolve", map[string]any{
		"action": "apply_selected",
		"hunks":  []map[string]any{{"path": "tracked.txt", "index": 0}},
	})
	if rec.Code != 200 {
		t.Fatalf("resolve status=%d body=%s", rec.Code, rec.Body.String())
	}
	resolved, err := a.store.Decision(context.Background(), decision.ID)
	if err != nil || resolved.Status != "approved" {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	content, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil || string(content) != "base\nchanged\n" {
		t.Fatalf("tracked content=%q err=%v", content, err)
	}
}

func TestReviewAPIConcurrentResolutionIsSingleWinner(t *testing.T) {
	a, root, run, base := reviewFixture(t)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proposal, err := a.server.proposeReview(context.Background(), scheduler.ReviewRequest{Job: &scheduler.Job{ProjectID: run.ProjectID, RunID: run.ID}, BaseCommit: base, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := a.store.Decision(context.Background(), proposal.DecisionID)
	if err != nil {
		t.Fatal(err)
	}
	cookie := bootstrapCookie(t, a)
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, action := range []string{"apply", "reject"} {
		wg.Add(1)
		go func(action string) {
			defer wg.Done()
			codes <- postJSON(t, a, cookie, "/api/v1/decisions/"+decision.ID+"/resolve", map[string]any{"action": action}).Code
		}(action)
	}
	wg.Wait()
	close(codes)
	winners := 0
	for code := range codes {
		if code == 200 {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("successful concurrent resolutions=%d, want 1", winners)
	}
	resolved, err := a.store.Decision(context.Background(), decision.ID)
	if err != nil || resolved.Status == "pending" {
		t.Fatalf("decision remained unresolved: %+v err=%v", resolved, err)
	}
}

func TestReviewAPIExpiryRestoresReviewedSnapshot(t *testing.T) {
	a, root, run, base := reviewFixture(t)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("agent edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proposal, err := a.server.proposeReview(context.Background(), scheduler.ReviewRequest{Job: &scheduler.Job{ProjectID: run.ProjectID, RunID: run.ID}, BaseCommit: base, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	a.server.expireReviewDecision(proposal.DecisionID)
	decision, err := a.store.Decision(context.Background(), proposal.DecisionID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != "denied" {
		t.Fatalf("expired decision status=%q, want denied", decision.Status)
	}
	content, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil || string(content) != "base\none\n" {
		t.Fatalf("expired snapshot content=%q err=%v", content, err)
	}
}

func TestReviewExpiryPreservesOperatorEditsAndUnblocksProject(t *testing.T) {
	a, root, run, base := reviewFixture(t)
	path := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(path, []byte("agent edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	proposal, err := a.server.proposeReview(context.Background(), scheduler.ReviewRequest{Job: &scheduler.Job{ProjectID: run.ProjectID, RunID: run.ID}, BaseCommit: base, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("operator edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.server.expireReviewDecision(proposal.DecisionID)
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "operator edit\n" {
		t.Fatalf("content=%q error=%v", content, err)
	}
	decision, err := a.store.Decision(context.Background(), proposal.DecisionID)
	if err != nil || decision.Status == "pending" {
		t.Fatalf("decision=%+v error=%v", decision, err)
	}
	final, err := a.store.Run(context.Background(), run.ID)
	if err != nil || final.Status != "failed" || !strings.Contains(final.Error, "manual review") {
		t.Fatalf("run=%+v error=%v", final, err)
	}
	if err := a.server.ensureNoPendingReview(context.Background(), run.ProjectID); err != nil {
		t.Fatal(err)
	}
}
