package api

import (
	"encoding/json"
	"github.com/10kkyvl/studioforge/internal/database"
	"testing"
)

func TestProjectUsageAPI(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	got := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/usage")
	if got.Code != 200 {
		t.Fatalf("status=%d %s", got.Code, got.Body.String())
	}
	var report database.UsageReport
	if err := json.Unmarshal(got.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Total <= 0 || len(report.Runs) == 0 || report.Days == nil || report.Weeks == nil {
		t.Fatalf("report=%+v", report)
	}
	missing := getJSON(t, a, cookie, "/api/v1/projects/missing/usage")
	if missing.Code != 404 {
		t.Fatalf("missing status=%d", missing.Code)
	}
}
