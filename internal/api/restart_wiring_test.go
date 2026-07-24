package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// A restart used to submit a bare Job: no thread, no validation opt-in, no
// stuck-detection settings. That left the restarted run invisible in the chat
// it belongs to and stripped of the safety net every other run gets, so this
// pins the wiring the successor must inherit.
func TestRestartInheritsThreadAndPerAgentWiring(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	ctx := context.Background()

	thread, err := a.store.EnsureDefaultThread(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.SQL.Exec("UPDATE runs SET status='interrupted', thread_id=? WHERE id='demo-obby-history'", thread.ID); err != nil {
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

	runs, err := a.store.ListRuns(ctx, "demo-obby", 100)
	if err != nil {
		t.Fatal(err)
	}
	var successor string
	for _, run := range runs {
		if run.ID != "demo-obby-history" && run.ThreadID == thread.ID {
			successor = run.ID
			break
		}
	}
	if successor == "" {
		t.Fatalf("the restarted run was not attached to the original thread %q; runs=%+v", thread.ID, runs)
	}
}
