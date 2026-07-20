package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
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
	if err != nil || len(agents) != 1 || agents[0].Provider != "codex" {
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
	request := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/settings", strings.NewReader(`{"default_provider":"codex","codex_path":"C:\\tools\\codex.exe","concurrency":"8"}`))
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
	if settings["default_provider"] != "codex" || settings["codex_path"] != `C:\tools\codex.exe` || settings["concurrency"] != "8" {
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
