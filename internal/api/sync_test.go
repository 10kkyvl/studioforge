package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

type fakeSyncer struct {
	gotProjectID, gotProjectFile string
	gotCtx                       context.Context
	startStatus                  models.SyncStatus
	startErr                     error
	stopProjectID                string
	stopErr                      error
	status                       map[string]models.SyncStatus
}

func (f *fakeSyncer) Start(ctx context.Context, projectID, projectFile string) (models.SyncStatus, error) {
	f.gotProjectID, f.gotProjectFile, f.gotCtx = projectID, projectFile, ctx
	return f.startStatus, f.startErr
}
func (f *fakeSyncer) Stop(projectID string) error {
	f.stopProjectID = projectID
	return f.stopErr
}
func (f *fakeSyncer) Status(projectID string) models.SyncStatus {
	return f.status[projectID]
}

// Without a configured syncer (the default in newTestAPI, matching a platform
// where rojo was never detected), the endpoint must say so rather than panic
// on a nil interface.
func TestStartAndStopSyncNotSupportedWithoutASyncer(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	start := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/sync", map[string]any{})
	if start.Code != 501 {
		t.Fatalf("start status=%d body=%s", start.Code, start.Body.String())
	}
	stop := deleteJSON(t, a, cookie, "/api/v1/projects/demo-obby/sync")
	if stop.Code != 501 {
		t.Fatalf("stop status=%d body=%s", stop.Code, stop.Body.String())
	}
}

func TestStartSyncUnknownProject(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.syncer = &fakeSyncer{}
	rec := postJSON(t, a, cookie, "/api/v1/projects/nope/sync", map[string]any{})
	if rec.Code != 404 {
		t.Fatalf("an unknown project must 404, status=%d", rec.Code)
	}
}

// A project whose default.project.json disappeared (moved, renamed, never
// scaffolded by an older import) must fail fast with a clear reason instead
// of handing rojo.Manager a path it will only fail on asynchronously inside
// the spawned process.
func TestStartSyncMissingProjectFile(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.syncer = &fakeSyncer{}
	project, err := a.store.Project(context.Background(), "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(project.Path, "default.project.json")); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/sync", map[string]any{})
	if rec.Code != 400 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStartSyncLaunchesAndReturnsTheSession(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeSyncer{startStatus: models.SyncStatus{Active: true, Port: 34872}}
	a.server.syncer = fake
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/sync", map[string]any{})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var status models.SyncStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Active || status.Port != 34872 {
		t.Errorf("status=%+v, want the syncer's session", status)
	}
	if fake.gotProjectID != "demo-obby" {
		t.Errorf("gotProjectID=%q", fake.gotProjectID)
	}
	if filepath.Base(fake.gotProjectFile) != "default.project.json" {
		t.Errorf("gotProjectFile=%q, want the project's default.project.json", fake.gotProjectFile)
	}
}

// A `rojo serve` session must outlive the HTTP request that started it, but
// net/http cancels a real request's context the instant ServeHTTP returns —
// httptest does not, so the only way to catch a handler that wired r.Context()
// into a long-lived subprocess is to cancel the request context ourselves
// afterward, exactly as the real server would, and check the syncer never saw
// that cancellation. This reproduces a real bug: an earlier version of
// startSync passed r.Context() straight to the syncer, which killed the
// spawned `rojo serve` process within moments of the response being sent —
// invisible to every other test here because none of them simulate the
// server cancelling the request context on return.
func TestStartSyncDetachesFromTheRequestContext(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeSyncer{startStatus: models.SyncStatus{Active: true, Port: 1}}
	a.server.syncer = fake

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects/demo-obby/sync", strings.NewReader("{}")).WithContext(ctx)
	req.Header.Set("Origin", "http://127.0.0.1:1234")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	cancel() // what net/http does to r.Context() right after ServeHTTP returns

	if fake.gotCtx == nil {
		t.Fatal("Start was never called")
	}
	if err := fake.gotCtx.Err(); err != nil {
		t.Fatalf("the context handed to Start must survive the request ending, got %v", err)
	}
}

func TestStartSyncSurfacesAManagerError(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.syncer = &fakeSyncer{startErr: errors.New("Rojo is already running for project demo-obby")}
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/sync", map[string]any{})
	if rec.Code != 409 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStopSyncStopsTheNamedProject(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeSyncer{status: map[string]models.SyncStatus{"demo-obby": {Active: true}}}
	a.server.syncer = fake
	rec := deleteJSON(t, a, cookie, "/api/v1/projects/demo-obby/sync")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.stopProjectID != "demo-obby" {
		t.Errorf("stopProjectID=%q", fake.stopProjectID)
	}
}

// Nothing to stop is the one Stop outcome the handler must actually refuse —
// checked against Status, not by asking Stop and inspecting its error (see
// the next test for why the error itself is not trustworthy signal).
func TestStopSyncRejectsWhenNothingIsRunning(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.syncer = &fakeSyncer{}
	rec := deleteJSON(t, a, cookie, "/api/v1/projects/demo-obby/sync")
	if rec.Code != 409 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// On Windows, gracefully closing a console subprocess with no window
// routinely fails even though the force-kill that follows it succeeds a
// moment later (internal/rojo's own tests log this error rather than treat it
// as one — see manager_test.go and real_smoke_test.go). Surfacing every such
// hiccup as a 409 would tell the operator a stop failed when the session is,
// in fact, on its way out, so the handler must not propagate Stop's error
// once Status already confirmed a session was there to stop.
func TestStopSyncToleratesATerminateHiccup(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.syncer = &fakeSyncer{
		status:  map[string]models.SyncStatus{"demo-obby": {Active: true}},
		stopErr: errors.New("exit status 255"),
	}
	rec := deleteJSON(t, a, cookie, "/api/v1/projects/demo-obby/sync")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// The project payload is where the spec says sync status belongs — not a
// separate polling endpoint — so the snapshot's projects must carry it.
func TestSnapshotProjectsCarryTheirSyncStatus(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.syncer = &fakeSyncer{status: map[string]models.SyncStatus{
		"demo-obby": {Active: true, Port: 34872},
	}}
	rec := getJSON(t, a, cookie, "/api/v1/snapshot")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Projects []models.Project `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, project := range body.Projects {
		if project.ID != "demo-obby" {
			if project.Sync.Active {
				t.Errorf("project %s reports sync active, want only demo-obby", project.ID)
			}
			continue
		}
		found = true
		if !project.Sync.Active || project.Sync.Port != 34872 {
			t.Errorf("demo-obby sync=%+v, want the syncer's status", project.Sync)
		}
	}
	if !found {
		t.Fatal("demo-obby missing from snapshot projects")
	}
}
