package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func deleteJSON(t *testing.T, a *testAPI, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("DELETE", "http://127.0.0.1:1234"+path, nil)
	req.Header.Set("Origin", "http://127.0.0.1:1234")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func TestCreateTaskDefaultsBacklog(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/tasks", map[string]any{"title": "New feature"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var task models.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.Status != "backlog" {
		t.Errorf("status=%q want %q", task.Status, "backlog")
	}
	if task.Priority != 50 {
		t.Errorf("priority=%d want 50", task.Priority)
	}
	if task.ProjectID != "demo-obby" {
		t.Errorf("projectId=%q want demo-obby", task.ProjectID)
	}
}

func TestCreateTaskRejectsBlankTitle(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/tasks", map[string]any{"title": "   "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank title must be refused, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateTaskRejectsUnknownProject(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postJSON(t, a, cookie, "/api/v1/projects/nope/tasks", map[string]any{"title": "New feature"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateTaskToRunning(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postJSON(t, a, cookie, "/api/v1/tasks/demo-obby-task-design", map[string]any{"status": "running"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var task models.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.Status != "running" {
		t.Errorf("status=%q want %q", task.Status, "running")
	}
}

func TestUpdateTaskRejectsBogusStatus(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postJSON(t, a, cookie, "/api/v1/tasks/demo-obby-task-design", map[string]any{"status": "not-a-status"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus status must be refused, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeleteTaskRemovesFromSnapshot(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	created := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/tasks", map[string]any{"title": "Temp"})
	var task models.Task
	if err := json.Unmarshal(created.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	rec := deleteJSON(t, a, cookie, "/api/v1/tasks/"+task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ok struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ok); err != nil || !ok.OK {
		t.Fatalf("ok body=%s err=%v", rec.Body.String(), err)
	}
	snapshot := getJSON(t, a, cookie, "/api/v1/snapshot")
	var body struct {
		Tasks []models.Task `json:"tasks"`
	}
	if err := json.Unmarshal(snapshot.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, remaining := range body.Tasks {
		if remaining.ID == task.ID {
			t.Errorf("deleted task %q still present in snapshot", task.ID)
		}
	}
}

func TestCreateRunAttachesTaskAndSetsRunning(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := createRunJSON(t, a, cookie, map[string]any{"projectId": "demo-obby", "prompt": "Do the thing", "taskId": "demo-obby-task-design"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var run models.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.TaskID != "demo-obby-task-design" {
		t.Errorf("run.TaskID=%q want %q", run.TaskID, "demo-obby-task-design")
	}
	task, err := a.store.Task(context.Background(), "demo-obby-task-design")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "running" {
		t.Errorf("attached task status=%q want %q", task.Status, "running")
	}
}

func TestCreateRunLeavesTaskStatusUntouchedWhenSubmitFails(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	before, err := a.store.Task(context.Background(), "demo-obby-task-design")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.scheduler.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := createRunJSON(t, a, cookie, map[string]any{"projectId": "demo-obby", "prompt": "Do the thing", "taskId": "demo-obby-task-design"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	after, err := a.store.Task(context.Background(), "demo-obby-task-design")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status {
		t.Errorf("task status changed to %q despite the run failing to submit, want unchanged %q", after.Status, before.Status)
	}
}

func TestCreateRunRejectsTaskFromAnotherProject(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := createRunJSON(t, a, cookie, map[string]any{"projectId": "demo-obby", "prompt": "Do the thing", "taskId": "demo-tycoon-task-design"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a task from another project must 400, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateProjectScaffoldsRojoSkeleton(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	projectPath := filepath.Join(t.TempDir(), "scaffolded-project")
	rec := postJSON(t, a, cookie, "/api/v1/projects", map[string]any{"name": "Scaffolded", "path": projectPath, "create": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(projectPath, "default.project.json")); err != nil {
		t.Fatalf("default.project.json not written: %v", err)
	}
}

func TestCreateProjectOpenStudioIsBestEffort(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	projectPath := filepath.Join(t.TempDir(), "open-after-create")
	// s.studio is nil in the test harness; openStudio:true must not fail the create.
	rec := postJSON(t, a, cookie, "/api/v1/projects", map[string]any{"name": "OpenMe", "path": projectPath, "create": true, "openStudio": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
