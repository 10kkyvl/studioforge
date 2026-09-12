package api

import (
	"context"
	"encoding/json"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/studiochanges"
	"net/http/httptest"
	"testing"
)

func TestStudioJournalAvailableWithoutGitAndAuthenticated(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()
	run, _, err := a.store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.StudioRecorder(run.ID).Start(ctx, "execute_luau", nil); err != nil {
		t.Fatal(err)
	}
	cookie := bootstrapCookie(t, a)
	for _, suffix := range []string{"studio-changes", "diff"} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/runs/"+run.ID+"/"+suffix, nil)
		unauth := httptest.NewRecorder()
		a.handler.ServeHTTP(unauth, req)
		if unauth.Code != 401 {
			t.Fatalf("unauth status=%d", unauth.Code)
		}
		req.AddCookie(cookie)
		res := httptest.NewRecorder()
		a.handler.ServeHTTP(res, req)
		if res.Code != 200 {
			t.Fatalf("%s: %d %s", suffix, res.Code, res.Body.String())
		}
		var changes []studiochanges.Change
		if suffix == "diff" {
			var body struct {
				StudioChanges []studiochanges.Change `json:"studioChanges"`
			}
			err = json.Unmarshal(res.Body.Bytes(), &body)
			changes = body.StudioChanges
		} else {
			err = json.Unmarshal(res.Body.Bytes(), &changes)
		}
		if err != nil || len(changes) != 1 || changes[0].RunID != run.ID {
			t.Fatalf("%s changes=%+v err=%v", suffix, changes, err)
		}
	}
}
