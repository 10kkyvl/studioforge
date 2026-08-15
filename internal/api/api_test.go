package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/diagnostics"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/platform/toolpath"
	"github.com/10kkyvl/studioforge/internal/projects"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/mock"
	"github.com/10kkyvl/studioforge/internal/resources"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

func fakeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

type testAPI struct {
	server    *Server
	handler   http.Handler
	store     *database.Store
	sessions  *SessionManager
	scheduler *scheduler.Manager
	hub       *events.Hub
	leases    *resources.Manager
	db        *database.DB
	cancel    context.CancelFunc
}

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	store := database.NewStore(db)
	data := t.TempDir()
	if err := store.SeedDemo(ctx, data); err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub(store)
	leases := resources.NewManager(time.Second)
	provider := mock.New()
	provider.StepDelay = 10 * time.Millisecond
	sched := scheduler.New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider})
	sessions, err := NewSessionManager(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	guard := projects.NewPathGuard()
	for _, project := range mustProjects(t, store) {
		_, _ = guard.Register(project.ID, project.Path)
	}
	server, err := New(Dependencies{Store: store, DB: db, Scheduler: sched, Hub: hub, Doctor: &diagnostics.Doctor{DB: db, DataDir: data, MockMode: true}, Sessions: sessions, Guard: guard, AllowedHost: "127.0.0.1:1234", DataDir: data, Leases: leases})
	if err != nil {
		t.Fatal(err)
	}
	result := &testAPI{server: server, handler: server.Handler(), store: store, sessions: sessions, scheduler: sched, hub: hub, leases: leases, db: db, cancel: cancel}
	t.Cleanup(func() {
		closeCtx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		_ = sched.Close(closeCtx)
		leases.Close()
		hub.Close()
		cancel()
		_ = db.Close()
	})
	return result
}
func mustProjects(t *testing.T, store *database.Store) []models.Project {
	t.Helper()
	items, err := store.ListProjects(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	return items
}
func bootstrapCookie(t *testing.T, a *testAPI) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"token": a.sessions.BootstrapToken()})
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/session/bootstrap", bytes.NewReader(body))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("bootstrap status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookies=%+v", cookies)
	}
	return cookies[0]
}
func TestSecurityBootstrapAndSnapshot(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/snapshot", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var snapshot map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot["projects"].([]any)) != 3 {
		t.Fatalf("snapshot=%v", snapshot)
	}
	if recorder.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("wildcard or other CORS header emitted")
	}
	if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatal("strict script policy missing")
	}
}
func TestRejectsHostOriginAndUnauthenticatedRequests(t *testing.T) {
	a := newTestAPI(t)
	cases := []*http.Request{httptest.NewRequest("GET", "http://evil.example/api/v1/snapshot", nil), httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/settings", strings.NewReader(`{"locale":"en"}`))}
	for _, request := range cases {
		if strings.Contains(request.URL.Host, "127.") {
			request.Host = "127.0.0.1:1234"
		}
		recorder := httptest.NewRecorder()
		a.handler.ServeHTTP(recorder, request)
		if recorder.Code < 400 {
			t.Fatalf("request %s unexpectedly allowed", request.URL)
		}
	}
}
func TestRunIdempotencyAndActions(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	submit := func() *httptest.ResponseRecorder {
		body := strings.NewReader(`{"projectId":"demo-obby","agentId":"demo-obby-orch","maxBudget":1,"prompt":"Build the first milestone"}`)
		request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs", body)
		request.Header.Set("Origin", "http://127.0.0.1:1234")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "same-run")
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		a.handler.ServeHTTP(recorder, request)
		return recorder
	}
	first, second := submit(), submit()
	if first.Code != 201 || second.Code != 200 {
		t.Fatalf("statuses=%d,%d bodies=%s %s", first.Code, second.Code, first.Body, second.Body)
	}
	var r1, r2 models.Run
	_ = json.Unmarshal(first.Body.Bytes(), &r1)
	_ = json.Unmarshal(second.Body.Bytes(), &r2)
	if r1.ID == "" || r1.ID != r2.ID {
		t.Fatalf("runs=%+v %+v", r1, r2)
	}
}

// postRun submits POST /api/v1/runs with the given task id (empty for none)
// and returns the recorder, so readiness tests can assert on both status and
// the error envelope's details payload.
func postRun(t *testing.T, a *testAPI, cookie *http.Cookie, taskID string) *httptest.ResponseRecorder {
	t.Helper()
	payload := map[string]any{
		"projectId": "demo-obby", "agentId": "demo-obby-orch", "maxBudget": 1,
		"prompt": "Build the first milestone",
	}
	if taskID != "" {
		payload["taskId"] = taskID
	}
	body, _ := json.Marshal(payload)
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs", bytes.NewReader(body))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "run-"+taskID+"-"+t.Name())
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	return recorder
}

// insertDanglingTaskDependency records a task_dependencies row whose
// depends_on_task_id does not exist in the tasks table, simulating a
// dependency left over after its target was deleted in some way the app
// itself never allows (the FK is ON DELETE CASCADE, so a normal delete
// through the API removes the tracking row too). It pins one physical
// connection with sql.Conn (rather than sql.Tx, since SQLite refuses to
// toggle the foreign_keys pragma inside a transaction) so the pragma and the
// insert land on the same connection.
func insertDanglingTaskDependency(t *testing.T, a *testAPI, projectID, taskID, missingDepID string) {
	t.Helper()
	ctx := context.Background()
	conn, err := a.db.SQL.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO task_dependencies(project_id,task_id,depends_on_task_id) VALUES(?,?,?)", projectID, taskID, missingDepID); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRunWithoutTaskIsUnaffected(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	recorder := postRun(t, a, cookie, "")
	if recorder.Code != 201 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateProjectRefusesToCreateADirectoryOutsideAnExistingParent(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	target := filepath.Join(t.TempDir(), "missing-parent", "project")
	payload, _ := json.Marshal(map[string]any{
		"name": "Outside Parent", "path": target, "create": true,
	})
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects", bytes.NewReader(payload))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 400 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("directory %s was created despite the missing parent", target)
	}
}

func TestCreateRunTaskWithNoDependenciesIsReady(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	recorder := postRun(t, a, cookie, "demo-obby-task-design")
	if recorder.Code != 201 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateRunTaskWithCompletedDependencyIsReady(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	// demo-obby-task-build depends only on demo-obby-task-design, seeded completed.
	recorder := postRun(t, a, cookie, "demo-obby-task-build")
	if recorder.Code != 201 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateRunTaskWithIncompleteDependencyIsRejected(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	before, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	// demo-obby-task-review depends on demo-obby-task-build, seeded "running".
	recorder := postRun(t, a, cookie, "demo-obby-task-review")
	if recorder.Code != 409 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details struct {
				Blockers []struct {
					TaskID string `json:"taskId"`
					Title  string `json:"title"`
					Status string `json:"status"`
				} `json:"blockers"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "task_dependencies_incomplete" {
		t.Fatalf("code=%q body=%s", envelope.Error.Code, recorder.Body.String())
	}
	if len(envelope.Error.Details.Blockers) != 1 || envelope.Error.Details.Blockers[0].TaskID != "demo-obby-task-build" || envelope.Error.Details.Blockers[0].Status != "running" {
		t.Fatalf("blockers=%+v", envelope.Error.Details.Blockers)
	}
	after, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("rejected run still queued a job: before=%d after=%d", len(before), len(after))
	}
}

func TestCreateRunTaskWithTransitiveIncompleteDependencyIsRejected(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	// A new task depending on demo-obby-task-review, which depends on
	// demo-obby-task-build ("running"), which depends on demo-obby-task-design
	// ("completed"): the walk must surface the incomplete ancestors, not just
	// the direct dependency.
	created, err := a.store.CreateTask(context.Background(), models.Task{ProjectID: "demo-obby", Title: "Ship it", Status: "backlog"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.AddTaskDependency(context.Background(), "demo-obby", created.ID, "demo-obby-task-review"); err != nil {
		t.Fatal(err)
	}
	recorder := postRun(t, a, cookie, created.ID)
	if recorder.Code != 409 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Error struct {
			Details struct {
				Blockers []struct{ TaskID, Status string } `json:"blockers"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, b := range envelope.Error.Details.Blockers {
		ids[b.TaskID] = b.Status
	}
	if ids["demo-obby-task-review"] != "blocked" || ids["demo-obby-task-build"] != "running" {
		t.Fatalf("blockers=%+v", envelope.Error.Details.Blockers)
	}
}

func TestCreateRunTaskWithMissingDependencyIsRejected(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	insertDanglingTaskDependency(t, a, "demo-obby", "demo-obby-task-design", "deleted-task-id")
	recorder := postRun(t, a, cookie, "demo-obby-task-design")
	if recorder.Code != 409 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Blockers []struct{ TaskID, Status string } `json:"blockers"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "task_dependencies_incomplete" {
		t.Fatalf("code=%q", envelope.Error.Code)
	}
	if len(envelope.Error.Details.Blockers) != 1 || envelope.Error.Details.Blockers[0].TaskID != "deleted-task-id" || envelope.Error.Details.Blockers[0].Status != "missing" {
		t.Fatalf("blockers=%+v", envelope.Error.Details.Blockers)
	}
}

func TestCreateRunTaskWithCrossProjectDependencyIsRejected(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	if err := a.store.AddTaskDependency(context.Background(), "demo-obby", "demo-obby-task-design", "demo-tycoon-task-design"); err != nil {
		t.Fatal(err)
	}
	recorder := postRun(t, a, cookie, "demo-obby-task-design")
	if recorder.Code != 409 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Error struct {
			Details struct {
				Blockers []struct{ TaskID, Title, Status string } `json:"blockers"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Error.Details.Blockers) != 1 || envelope.Error.Details.Blockers[0].TaskID != "demo-tycoon-task-design" || envelope.Error.Details.Blockers[0].Status != "missing" {
		t.Fatalf("blockers=%+v", envelope.Error.Details.Blockers)
	}
	if envelope.Error.Details.Blockers[0].Title != "" {
		t.Fatalf("cross-project task title leaked into response: %+v", envelope.Error.Details.Blockers[0])
	}
}

func TestCreateRunRecheckCatchesDependencyStatusChangedAfterInitialCheck(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	t.Cleanup(func() { testHookAfterInitialTaskReadinessCheck = nil })
	testHookAfterInitialTaskReadinessCheck = func() {
		if _, err := a.db.SQL.Exec("UPDATE tasks SET status='running' WHERE id='demo-obby-task-design'"); err != nil {
			t.Fatal(err)
		}
	}
	before, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	// demo-obby-task-build's only dependency (demo-obby-task-design) passes the
	// initial check as "completed", then the hook above flips it to "running"
	// before the recheck immediately before scheduler submission runs.
	recorder := postRun(t, a, cookie, "demo-obby-task-build")
	if recorder.Code != 409 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "task_dependencies_incomplete") {
		t.Fatalf("body=%s", recorder.Body.String())
	}
	after, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("race-losing request still queued a job: before=%d after=%d", len(before), len(after))
	}
}

func TestRunRestartRechecksTaskReadiness(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	submitted := postRun(t, a, cookie, "demo-obby-task-build")
	if submitted.Code != 201 {
		t.Fatalf("submit status=%d body=%s", submitted.Code, submitted.Body.String())
	}
	var run models.Run
	if err := json.Unmarshal(submitted.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, a.store, run.ID, "completed", 10*time.Second)
	if _, err := a.db.SQL.Exec("UPDATE runs SET status='interrupted' WHERE id=?", run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.SQL.Exec("UPDATE tasks SET status='running' WHERE id='demo-obby-task-design'"); err != nil {
		t.Fatal(err)
	}
	before, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs/"+run.ID+"/restart", strings.NewReader(`{}`))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 409 || !strings.Contains(recorder.Body.String(), "task_dependencies_incomplete") {
		t.Fatalf("restart status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	after, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("blocked restart still queued a job: before=%d after=%d", len(before), len(after))
	}
}

func TestRunResumeDoesNotRecheckTaskReadiness(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	submitted := postRun(t, a, cookie, "demo-obby-task-build")
	if submitted.Code != 201 {
		t.Fatalf("submit status=%d body=%s", submitted.Code, submitted.Body.String())
	}
	var run models.Run
	if err := json.Unmarshal(submitted.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, a.store, run.ID, "completed", 10*time.Second)
	if _, err := a.db.SQL.Exec("UPDATE runs SET status='paused' WHERE id=?", run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.SQL.Exec("UPDATE tasks SET status='running' WHERE id='demo-obby-task-design'"); err != nil {
		t.Fatal(err)
	}
	before, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs/"+run.ID+"/resume", strings.NewReader(`{}`))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("resume status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	after, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("resume did not continue the lineage: before=%d after=%d", len(before), len(after))
	}
}

func waitRunStatus(t *testing.T, store *database.Store, id, status string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		run, err := store.Run(context.Background(), id)
		if err == nil {
			last = run.Status
			if last == status {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s status=%s wanted=%s", id, last, status)
}

type reviewFlagOpenCall struct {
	runID, projectID, checkpoint string
}

type reviewFlagRecordingGate struct {
	mu    sync.Mutex
	calls []reviewFlagOpenCall
}

func (g *reviewFlagRecordingGate) Open(_ context.Context, runID, projectID, checkpoint string, _ time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, reviewFlagOpenCall{runID, projectID, checkpoint})
	return nil
}
func (g *reviewFlagRecordingGate) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

type editingProvider struct{}

func (p *editingProvider) Diagnose(context.Context) providers.Diagnostics {
	return providers.Diagnostics{Available: true, Authenticated: true}
}
func (p *editingProvider) Start(context.Context, providers.RunRequest) (providers.RunHandle, error) {
	h := &editingHandle{events: make(chan providers.Event, 4), done: make(chan struct{})}
	go h.run()
	return h, nil
}
func (p *editingProvider) Resume(ctx context.Context, req providers.ResumeRequest) (providers.RunHandle, error) {
	return p.Start(ctx, req.RunRequest)
}
func (p *editingProvider) Cancel(context.Context, string) error { return nil }

type editingHandle struct {
	events chan providers.Event
	done   chan struct{}
	result providers.Result
}

func (h *editingHandle) run() {
	defer close(h.events)
	defer close(h.done)
	h.events <- providers.Event{Type: "message", RawType: "assistant", Payload: map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "tool_use", "id": "toolu_1", "name": "Edit", "input": map[string]any{"file_path": "script.lua"}},
			},
		},
	}, At: time.Now().UTC()}
	h.result = providers.Result{SessionID: "edit-session", ExitCode: 0}
}
func (h *editingHandle) Events() <-chan providers.Event { return h.events }
func (h *editingHandle) Wait() providers.Result         { <-h.done; return h.result }
func (h *editingHandle) Cancel() error                  { return nil }

func TestRunJobCarriesTheReviewGateFlagAndBaseCheckpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "review-flag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := database.NewStore(db)

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "StudioForge Test")
	if err := os.WriteFile(filepath.Join(repo, "script.lua"), []byte("print('v1')\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(repo, "script.lua"), []byte("print('v2')\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	project, err := store.CreateProject(ctx, models.Project{Name: "review-flag", Path: repo, Fingerprint: "review-flag"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.CreateAgent(ctx, models.Agent{ProjectID: project.ID, ReviewBeforeApply: true})
	if err != nil {
		t.Fatal(err)
	}

	hub := events.NewHub(store)
	defer hub.Close()
	leases := resources.NewManager(time.Second)
	defer leases.Close()
	sched := scheduler.New(ctx, store, hub, leases, map[string]providers.Provider{agent.Provider: &editingProvider{}})
	gate := &reviewFlagRecordingGate{}
	sched.SetReviewGate(gate)
	sessions, err := NewSessionManager(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	guard := projects.NewPathGuard()
	if _, err := guard.Register(project.ID, project.Path); err != nil {
		t.Fatal(err)
	}
	server, err := New(Dependencies{Store: store, DB: db, Scheduler: sched, Hub: hub, Doctor: &diagnostics.Doctor{DB: db, DataDir: t.TempDir(), MockMode: true}, Sessions: sessions, Guard: guard, AllowedHost: "127.0.0.1:1234", DataDir: t.TempDir(), Leases: leases})
	if err != nil {
		t.Fatal(err)
	}
	a := &testAPI{server: server, handler: server.Handler(), store: store, sessions: sessions, scheduler: sched, hub: hub, leases: leases, db: db, cancel: cancel}
	cookie := bootstrapCookie(t, a)

	body, _ := json.Marshal(map[string]any{"projectId": project.ID, "agentId": agent.ID, "maxBudget": 5, "prompt": "Build the first milestone"})
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs", bytes.NewReader(body))
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
	waitRunStatus(t, store, run.ID, "waiting_decision", 3*time.Second)

	if gate.callCount() != 1 {
		t.Fatalf("Open call count=%d, want 1 (ReviewBeforeApply must have reached the Job)", gate.callCount())
	}
	call := gate.calls[0]
	if call.runID != run.ID || call.projectID != project.ID {
		t.Fatalf("Open call=%+v", call)
	}
	if call.checkpoint == "" {
		t.Fatal("Open call checkpoint is empty, want the run's BaseCheckpoint to have reached the Job")
	}
	checkpoint, err := store.CheckpointForRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if call.checkpoint != checkpoint.CommitHash {
		t.Fatalf("Open call checkpoint=%q, want the persisted checkpoint %q", call.checkpoint, checkpoint.CommitHash)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestCancelReturns202AndRunIsImmediatelyRestartable(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
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

	cancelRequest := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs/"+run.ID+"/cancel", strings.NewReader(`{}`))
	cancelRequest.Header.Set("Origin", "http://127.0.0.1:1234")
	cancelRequest.Header.Set("Content-Type", "application/json")
	cancelRequest.AddCookie(cookie)
	cancelRecorder := httptest.NewRecorder()
	a.handler.ServeHTTP(cancelRecorder, cancelRequest)
	if cancelRecorder.Code != 202 {
		t.Fatalf("cancel status=%d body=%s", cancelRecorder.Code, cancelRecorder.Body.String())
	}
	waitRunStatus(t, a.store, run.ID, "cancelled", 5*time.Second)

	restartRequest := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs/"+run.ID+"/restart", strings.NewReader(`{}`))
	restartRequest.Header.Set("Origin", "http://127.0.0.1:1234")
	restartRequest.Header.Set("Content-Type", "application/json")
	restartRequest.AddCookie(cookie)
	restartRecorder := httptest.NewRecorder()
	a.handler.ServeHTTP(restartRecorder, restartRequest)
	if restartRecorder.Code != 200 {
		t.Fatalf("restart status=%d body=%s", restartRecorder.Code, restartRecorder.Body.String())
	}
}

func TestProjectCreationAddsDefaultAgentAndAgentCRUD(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	projectPath := filepath.Join(t.TempDir(), "new-project")
	body, _ := json.Marshal(map[string]any{"name": "New project", "path": projectPath, "create": true})
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects", bytes.NewReader(body))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var project models.Project
	if err := json.Unmarshal(recorder.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	agents, err := a.store.ListAgents(context.Background(), project.ID)
	if err != nil || len(agents) != 1 || agents[0].Provider != "claude" {
		t.Fatalf("agents=%+v err=%v", agents, err)
	}

	agentBody := `{"name":"Reviewer","role":"QA","provider":"mock","modelAlias":"fast","effort":"low","permission":"read-only","concurrency":1,"budget":2}`
	request = httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects/"+project.ID+"/agents", strings.NewReader(agentBody))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create agent status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var agent models.Agent
	_ = json.Unmarshal(recorder.Body.Bytes(), &agent)
	agent.Enabled = false
	updateBody, _ := json.Marshal(agent)
	request = httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects/"+project.ID+"/agents/"+agent.ID, bytes.NewReader(updateBody))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update agent status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAgentNetworkPolicyValidation(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	projectPath := filepath.Join(t.TempDir(), "network-policy-project")
	body, _ := json.Marshal(map[string]any{"name": "Network policy project", "path": projectPath, "create": true})
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects", bytes.NewReader(body))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var project models.Project
	if err := json.Unmarshal(recorder.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}

	badBody := `{"name":"Bad Policy","role":"QA","provider":"mock","modelAlias":"fast","effort":"low","permission":"read-only","networkPolicy":"nonsense","concurrency":1,"budget":2}`
	request = httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects/"+project.ID+"/agents", strings.NewReader(badBody))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400 for an unrecognized network policy", recorder.Code, recorder.Body.String())
	}

	defaultBody := `{"name":"Default Policy","role":"QA","provider":"mock","modelAlias":"fast","effort":"low","permission":"read-only","concurrency":1,"budget":2}`
	request = httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects/"+project.ID+"/agents", strings.NewReader(defaultBody))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create agent status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var agent models.Agent
	if err := json.Unmarshal(recorder.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	if agent.NetworkPolicy != "unrestricted" {
		t.Fatalf("networkPolicy=%q, want the default of unrestricted when the field is omitted", agent.NetworkPolicy)
	}
}

func TestRuntimeSettingsAreValidatedAndReturned(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	claudePath := fakeExecutable(t, t.TempDir(), "claude")
	normalized, err := toolpath.Validate("claude_path", claudePath)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"default_provider": "openrouter", "claude_path": claudePath, "concurrency": "8", "playtest_poll_seconds": "5"})
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/settings", bytes.NewReader(body))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/snapshot", nil)
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	var snapshot map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	settings := snapshot["settings"].(map[string]any)
	if settings["default_provider"] != "openrouter" || settings["claude_path"] != normalized || settings["concurrency"] != "8" || settings["playtest_poll_seconds"] != "5" {
		t.Fatalf("settings=%+v want normalized claude_path=%q", settings, normalized)
	}
}

func TestSettingsRejectInvalidToolPaths(t *testing.T) {
	dir := t.TempDir()
	realGit := fakeExecutable(t, dir, "git")
	cases := []struct {
		name  string
		value string
	}{
		{"a directory", dir},
		{"a missing file", filepath.Join(dir, "does-not-exist.exe")},
		{"a path with arguments appended", realGit + " --upload-pack=evil"},
		{"a value containing a newline", realGit + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAPI(t)
			cookie := bootstrapCookie(t, a)
			body, _ := json.Marshal(map[string]string{"locale": "ru", "git_path": tc.value})
			request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/settings", bytes.NewReader(body))
			request.Header.Set("Origin", "http://127.0.0.1:1234")
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(cookie)
			recorder := httptest.NewRecorder()
			a.handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var response struct {
				Error struct{ Code string } `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error.Code != "invalid_tool_path" {
				t.Errorf("code=%q want invalid_tool_path", response.Error.Code)
			}
			if _, ok, _ := a.store.Setting(context.Background(), "git_path"); ok {
				t.Error("git_path must not be persisted when the request is rejected")
			}
			if _, ok, _ := a.store.Setting(context.Background(), "locale"); ok {
				t.Error("locale must not be persisted either: a rejected request must not partially apply")
			}
		})
	}
}

func TestInvalidSettingsRequestDoesNotPartiallyPersist(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	if err := a.store.SetSetting(context.Background(), "locale", "en"); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/settings", strings.NewReader(`{"locale":"ru","concurrency":"0"}`))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("settings status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	locale, ok, err := a.store.Setting(context.Background(), "locale")
	if err != nil || !ok || locale != "en" {
		t.Fatalf("locale=%q ok=%v err=%v", locale, ok, err)
	}
}

func TestInvalidPlaytestPollSecondsIsRejected(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	if err := a.store.SetSetting(context.Background(), "locale", "en"); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/settings", strings.NewReader(`{"locale":"ru","playtest_poll_seconds":"61"}`))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("settings status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	locale, ok, err := a.store.Setting(context.Background(), "locale")
	if err != nil || !ok || locale != "en" {
		t.Fatalf("locale=%q ok=%v err=%v", locale, ok, err)
	}
}

func TestInterruptedRunCanRestart(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	if _, err := a.db.SQL.Exec("UPDATE runs SET status='interrupted' WHERE id='demo-obby-history'"); err != nil {
		t.Fatal(err)
	}
	before, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs/demo-obby-history/restart", strings.NewReader(`{}`))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	after, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 || after[0].ID == "demo-obby-history" {
		t.Fatalf("restart did not create an auditable successor: before=%d after=%d newest=%+v", len(before), len(after), after[0])
	}
}

func TestSafeModeBlocksEveryEndpointThatStartsAWorker(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)

	correctionJob, err := json.Marshal(scheduler.Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", TaskID: "", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), Prompt: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := a.store.CreateDecision(context.Background(), models.Decision{
		ProjectID: "demo-obby", RunID: "demo-obby-history", Kind: "correction_run",
		Summary: "Correction run proposed", Detail: "limit reached", Payload: string(correctionJob),
	})
	if err != nil {
		t.Fatal(err)
	}

	a.server.safeMode = true
	a.scheduler.SetSafeMode(true)
	before, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, method, path, body string
	}{
		{"create run", "POST", "/api/v1/runs", `{"projectId":"demo-obby","agentId":"demo-obby-orch","maxBudget":1,"prompt":"Build the first milestone"}`},
		{"resume", "POST", "/api/v1/runs/demo-obby-history/resume", `{}`},
		{"restart", "POST", "/api/v1/runs/demo-obby-history/restart", `{}`},
		{"approve decision", "POST", "/api/v1/decisions/" + decision.ID + "/resolve", `{"approve":true}`},
		{"start sync", "POST", "/api/v1/projects/demo-obby/sync", `{}`},
		{"open studio", "POST", "/api/v1/projects/demo-obby/open-studio", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "http://127.0.0.1:1234"+tc.path, strings.NewReader(tc.body))
			request.Header.Set("Origin", "http://127.0.0.1:1234")
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(cookie)
			recorder := httptest.NewRecorder()
			a.handler.ServeHTTP(recorder, request)
			if recorder.Code != 409 {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "safe_mode") {
				t.Fatalf("did not report safe_mode: %s", recorder.Body.String())
			}
		})
	}
	after, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("safe mode still queued a run: before=%d after=%d", len(before), len(after))
	}
}

func TestSafeModeStillAllowsPauseAndCancel(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	post := func(path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "http://127.0.0.1:1234"+path, strings.NewReader(body))
		request.Header.Set("Origin", "http://127.0.0.1:1234")
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		a.handler.ServeHTTP(recorder, request)
		return recorder
	}
	submitRecorder := post("/api/v1/runs", `{"projectId":"demo-obby","agentId":"demo-obby-orch","maxBudget":1,"prompt":"Build the first milestone"}`)
	if submitRecorder.Code != 201 {
		t.Fatalf("submit status=%d body=%s", submitRecorder.Code, submitRecorder.Body.String())
	}
	var run models.Run
	if err := json.Unmarshal(submitRecorder.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, a.store, run.ID, "running", 5*time.Second)

	a.server.safeMode = true

	pauseRecorder := post("/api/v1/runs/"+run.ID+"/pause", `{}`)
	if pauseRecorder.Code != 200 {
		t.Fatalf("pause in safe mode: status=%d body=%s", pauseRecorder.Code, pauseRecorder.Body.String())
	}
	waitRunStatus(t, a.store, run.ID, "paused", 5*time.Second)

	cancelRecorder := post("/api/v1/runs/"+run.ID+"/cancel", `{}`)
	if cancelRecorder.Code != 202 {
		t.Fatalf("cancel in safe mode: status=%d body=%s", cancelRecorder.Code, cancelRecorder.Body.String())
	}
	waitRunStatus(t, a.store, run.ID, "cancelled", 5*time.Second)
}

type cancelWriter struct {
	header http.Header
	body   bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *cancelWriter) WriteHeader(int) {}
func (w *cancelWriter) Write(body []byte) (int, error) {
	n, err := w.body.Write(body)
	if bytes.Contains(body, []byte("data:")) {
		w.cancel()
	}
	return n, err
}
func (w *cancelWriter) Flush() {}
func TestSSEReplaysPersistedEvents(t *testing.T) {
	a := newTestAPI(t)
	_, err := a.store.AppendEvents(context.Background(), []models.RunEvent{{ProjectID: "demo-obby", RunID: "demo-obby-history", AgentID: "demo-obby-orch", Type: "message", Payload: map[string]string{"text": "replayed"}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	writer := &cancelWriter{cancel: cancel}
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/events?runId=demo-obby-history", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { a.server.sse(writer, request); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE did not stop")
	}
	if !strings.Contains(writer.body.String(), "replayed") || !strings.Contains(writer.body.String(), "id: ") {
		t.Fatalf("stream=%s", writer.body.String())
	}
}
func TestSSELastEventIDHeaderTakesPriorityOverAfterQueryParam(t *testing.T) {
	a := newTestAPI(t)
	appended, err := a.store.AppendEvents(context.Background(), []models.RunEvent{
		{ProjectID: "demo-obby", RunID: "demo-obby-history", AgentID: "demo-obby-orch", Type: "message", Payload: map[string]string{"text": "seen-already"}},
		{ProjectID: "demo-obby", RunID: "demo-obby-history", AgentID: "demo-obby-orch", Type: "message", Payload: map[string]string{"text": "not-yet-seen"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	writer := &cancelWriter{cancel: cancel}
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/events?runId=demo-obby-history&after=0", nil).WithContext(ctx)
	request.Header.Set("Last-Event-ID", strconv.FormatInt(appended[0].ID, 10))
	done := make(chan struct{})
	go func() { a.server.sse(writer, request); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE did not stop")
	}
	if strings.Contains(writer.body.String(), "seen-already") {
		t.Fatalf("Last-Event-ID header was not honored, replayed an already-seen event: stream=%s", writer.body.String())
	}
	if !strings.Contains(writer.body.String(), "not-yet-seen") {
		t.Fatalf("expected the event after Last-Event-ID to be replayed: stream=%s", writer.body.String())
	}
}

type replayGapWriter struct {
	header  http.Header
	body    bytes.Buffer
	mu      sync.Mutex
	once    sync.Once
	started chan struct{}
	release chan struct{}
	cancel  context.CancelFunc
}

func (w *replayGapWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *replayGapWriter) WriteHeader(int) {}
func (w *replayGapWriter) Write(body []byte) (int, error) {
	w.mu.Lock()
	n, err := w.body.Write(body)
	w.mu.Unlock()
	if bytes.Contains(body, []byte("replayed")) {
		w.once.Do(func() { close(w.started) })
		<-w.release
	}
	if bytes.Contains(body, []byte("live-during-replay")) {
		w.cancel()
	}
	return n, err
}
func (w *replayGapWriter) Flush() {}

func TestSSEDoesNotLoseEventPublishedDuringReplay(t *testing.T) {
	a := newTestAPI(t)
	_, err := a.store.AppendEvents(context.Background(), []models.RunEvent{{ProjectID: "demo-obby", RunID: "demo-obby-history", Type: "message", Payload: map[string]string{"text": "replayed"}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	writer := &replayGapWriter{started: make(chan struct{}), release: make(chan struct{}), cancel: cancel}
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/events?runId=demo-obby-history", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { a.server.sse(writer, request); close(done) }()
	select {
	case <-writer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("replay did not start")
	}
	if _, err := a.server.hub.Publish(context.Background(), models.RunEvent{ProjectID: "demo-obby", RunID: "demo-obby-history", Type: "message", Payload: map[string]string{"text": "live-during-replay"}}); err != nil {
		t.Fatal(err)
	}
	close(writer.release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE did not deliver buffered live event")
	}
	writer.mu.Lock()
	stream := writer.body.String()
	writer.mu.Unlock()
	if !strings.Contains(stream, "replayed") || !strings.Contains(stream, "live-during-replay") {
		t.Fatalf("stream=%s", stream)
	}
}
func TestStaticMissingAssetWithExtensionReturns404(t *testing.T) {
	a := newTestAPI(t)
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/_app/immutable/chunks/DOESNOTEXIST.js", nil)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 404 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
func TestStaticNavigationalRouteFallsBackToIndexHTML(t *testing.T) {
	a := newTestAPI(t)
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/settings", nil)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("content-type=%s", contentType)
	}
	if !strings.Contains(strings.ToLower(recorder.Body.String()), "<!doctype html>") {
		t.Fatalf("body=%s", recorder.Body.String())
	}
}
func TestStaticExistingJSAssetServedWithCorrectContentType(t *testing.T) {
	a := newTestAPI(t)
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/bootstrap.js", nil)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	contentType := recorder.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/javascript") && !strings.HasPrefix(contentType, "application/javascript") {
		t.Fatalf("content-type=%s", contentType)
	}
}
func TestStaticIndexAndFallbackUseNoCacheHeader(t *testing.T) {
	a := newTestAPI(t)
	for _, path := range []string{"/", "/index.html"} {
		request := httptest.NewRequest("GET", "http://127.0.0.1:1234"+path, nil)
		recorder := httptest.NewRecorder()
		a.handler.ServeHTTP(recorder, request)
		if recorder.Code != 200 {
			t.Fatalf("path=%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
		if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "no-cache" {
			t.Fatalf("path=%s cache-control=%s", path, cacheControl)
		}
	}
}
func TestStaticImmutableAssetUsesLongLivedCacheHeader(t *testing.T) {
	a := newTestAPI(t)
	var assetPath string
	if err := fs.WalkDir(a.server.assets, "_app/immutable", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && assetPath == "" {
			assetPath = path
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if assetPath == "" {
		t.Fatal("no embedded assets found under _app/immutable")
	}
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/"+assetPath, nil)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "public, max-age=31536000, immutable" {
		t.Fatalf("cache-control=%s", cacheControl)
	}
}

func TestAgentReviewBeforeApplyDefaultsToOff(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	projectPath := filepath.Join(t.TempDir(), "review-gate-project")
	body, _ := json.Marshal(map[string]any{"name": "Review gate project", "path": projectPath, "create": true})
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects", bytes.NewReader(body))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var project models.Project
	if err := json.Unmarshal(recorder.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}

	agentBody := `{"name":"No Gate","role":"QA","provider":"mock","modelAlias":"fast","effort":"low","permission":"read-only","concurrency":1,"budget":2}`
	request = httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects/"+project.ID+"/agents", strings.NewReader(agentBody))
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create agent status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var agent models.Agent
	if err := json.Unmarshal(recorder.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	if agent.ReviewBeforeApply {
		t.Fatalf("reviewBeforeApply=%v, want the default of off when the field is omitted", agent.ReviewBeforeApply)
	}
}

func TestReviewGateExpiryHoursIsValidated(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  int
	}{
		{"zero hours is rejected", "0", http.StatusBadRequest},
		{"200 hours is rejected", "200", http.StatusBadRequest},
		{"24 hours is accepted", "24", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAPI(t)
			cookie := bootstrapCookie(t, a)
			body, _ := json.Marshal(map[string]string{"review_gate_expiry_hours": tc.value})
			request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/settings", bytes.NewReader(body))
			request.Header.Set("Origin", "http://127.0.0.1:1234")
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(cookie)
			recorder := httptest.NewRecorder()
			a.handler.ServeHTTP(recorder, request)
			if recorder.Code != tc.want {
				t.Fatalf("value=%s status=%d want=%d body=%s", tc.value, recorder.Code, tc.want, recorder.Body.String())
			}
		})
	}
}
