package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDetectPathsRequiresASession(t *testing.T) {
	a := newTestAPI(t)
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/detect-paths", nil)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 401 {
		t.Fatalf("path detection must require a session, status=%d", recorder.Code)
	}
}

func TestDetectPathsReportsEveryTool(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/detect-paths", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Tools map[string][]struct {
			Path, Source, Status string
		} `json:"tools"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"claude_path", "codex_path", "rojo_path", "git_path", "studio_mcp_path"} {
		if _, ok := body.Tools[tool]; !ok {
			t.Errorf("response omitted %q", tool)
		}
	}
	// Whatever is installed, a reported candidate must be usable as a setting.
	for tool, candidates := range body.Tools {
		for _, candidate := range candidates {
			if candidate.Path == "" {
				t.Errorf("%s: candidate without a path", tool)
			}
			if candidate.Status != "ok" && candidate.Status != "error" {
				t.Errorf("%s: unexpected status %q", tool, candidate.Status)
			}
		}
	}
}

// An operator who opens Settings and navigates away cancels the request while
// it is probing, which marks every candidate as failed. That result must not be
// cached, or the next half minute of Settings loads reports working tools as
// broken and quietly stops autofilling.
func TestDetectPathsDoesNotCacheACancelledProbe(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/detect-paths", nil).WithContext(cancelled)
	request.AddCookie(cookie)
	first := httptest.NewRecorder()
	a.handler.ServeHTTP(first, request)
	if first.Code == 200 {
		t.Fatalf("a cancelled probe must not be served as a result: %s", first.Body.String())
	}

	// The next caller must get a real detection rather than the poisoned one.
	second := httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/detect-paths", nil)
	second.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, second)
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Tools map[string][]struct{ Status, Message string } `json:"tools"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for tool, candidates := range body.Tools {
		for _, candidate := range candidates {
			if strings.Contains(candidate.Message, "context canceled") {
				t.Errorf("%s: served a cancelled probe from cache: %q", tool, candidate.Message)
			}
		}
	}
}

// The endpoint executes what it finds in order to read a version, so it must
// never take a path from the caller.
func TestDetectPathsRefusesCallerSuppliedInput(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	request := httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/detect-paths?path=C:\\evil.exe", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != 400 {
		t.Fatalf("caller-supplied input must be refused, status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
