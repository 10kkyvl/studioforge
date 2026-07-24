package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/diagnostics"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/projects"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/mock"
	"github.com/10kkyvl/studioforge/internal/resources"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

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

func TestRuntimeSettingsAreValidatedAndReturned(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/settings", strings.NewReader(`{"default_provider":"openrouter","claude_path":"C:\\tools\\claude.exe","concurrency":"8"}`))
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
	if settings["default_provider"] != "openrouter" || settings["claude_path"] != `C:\tools\claude.exe` || settings["concurrency"] != "8" {
		t.Fatalf("settings=%+v", settings)
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

func TestSafeModeBlocksActionsThatStartWorkers(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.safeMode = true
	if _, err := a.db.SQL.Exec("UPDATE runs SET status='interrupted' WHERE id='demo-obby-history'"); err != nil {
		t.Fatal(err)
	}
	before, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	// Resume and restart put a worker back on the queue, so safe mode must refuse them.
	// Pause and cancel only stop existing work and stay available.
	for _, action := range []string{"resume", "restart"} {
		request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs/demo-obby-history/"+action, strings.NewReader(`{}`))
		request.Header.Set("Origin", "http://127.0.0.1:1234")
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		a.handler.ServeHTTP(recorder, request)
		if recorder.Code != 409 {
			t.Fatalf("%s in safe mode: status=%d body=%s", action, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "safe_mode") {
			t.Fatalf("%s in safe mode did not report safe_mode: %s", action, recorder.Body.String())
		}
	}
	after, err := a.store.ListRuns(context.Background(), "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("safe mode still queued a run: before=%d after=%d", len(before), len(after))
	}
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
