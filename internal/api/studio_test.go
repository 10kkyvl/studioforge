package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/10kkyvl/studioforge/internal/roblox/studio"
)

type fakeOpener struct {
	gotPath string
	gotName string
	gotID   string
	place   string
	err     error
}

func (f *fakeOpener) OpenProject(_ context.Context, projectPath, name, id string) (string, error) {
	f.gotPath, f.gotName, f.gotID = projectPath, name, id
	return f.place, f.err
}

func TestOpenStudioLaunchesTheProject(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeOpener{place: "C:\\place.rbxl"}
	a.server.studio = fake
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/open-studio", map[string]any{})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Place string `json:"place"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Place != "C:\\place.rbxl" {
		t.Errorf("place=%q want the opener's place", body.Place)
	}
	if fake.gotPath == "" {
		t.Error("the handler must hand the project's path to the opener")
	}
	// The opener names the place after the project, so it needs both.
	if fake.gotID != "demo-obby" || fake.gotName == "" {
		t.Errorf("the handler must hand the project's identity to the opener, name=%q id=%q", fake.gotName, fake.gotID)
	}
}

func TestOpenStudioUnknownProject(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.studio = &fakeOpener{}
	rec := postJSON(t, a, cookie, "/api/v1/projects/nope/open-studio", map[string]any{})
	if rec.Code != 404 {
		t.Fatalf("an unknown project must 404, status=%d", rec.Code)
	}
}

// This project's place already being open must be treated as "already
// there", not relaunched — the same rule the Studio MCP provisioner's
// auto-open already applies before opening Studio for a run.
func TestOpenStudioSkipsRelaunchWhenAlreadyMatched(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeOpener{place: "C:\\place.rbxl"}
	a.server.studio = fake
	a.server.studioOpenCheck = func(context.Context, string) (StudioOpenCheck, error) {
		return StudioOpenCheck{Matched: true, Place: "C:\\already-open.rbxl"}, nil
	}
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/open-studio", map[string]any{})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotPath != "" {
		t.Error("an already-open project's place must not be relaunched")
	}
	var body struct {
		Place       string `json:"place"`
		AlreadyOpen bool   `json:"alreadyOpen"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.AlreadyOpen || body.Place != "C:\\already-open.rbxl" {
		t.Errorf("body=%+v", body)
	}
}

// Other Studio instances being open, but none holding this project's place,
// must refuse rather than launch a possibly-duplicate window — with the
// diagnosable notice, not a bare failure.
func TestOpenStudioRefusesWhenOtherInstancesAreOpen(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeOpener{place: "C:\\place.rbxl"}
	a.server.studio = fake
	notice := "Studio MCP withheld: the open Studio does not hold this project's place (expected mine.rbxl, found other.rbxl); open the project's place, or close the others and let StudioForge open it automatically"
	a.server.studioOpenCheck = func(context.Context, string) (StudioOpenCheck, error) {
		return StudioOpenCheck{Notice: notice}, nil
	}
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/open-studio", map[string]any{})
	if rec.Code != 409 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotPath != "" {
		t.Error("a mismatched Studio must not be relaunched")
	}
	if !strings.Contains(rec.Body.String(), "does not hold this project's place") {
		t.Errorf("body=%s", rec.Body.String())
	}
}

// Nothing open at all is the safe case: the button must still launch.
func TestOpenStudioLaunchesWhenCheckReportsNothingOpen(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeOpener{place: "C:\\place.rbxl"}
	a.server.studio = fake
	a.server.studioOpenCheck = func(context.Context, string) (StudioOpenCheck, error) {
		return StudioOpenCheck{Open: true}, nil
	}
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/open-studio", map[string]any{})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotPath == "" {
		t.Error("nothing open should still launch")
	}
}

// A check that fails to run at all (no probe wired, or the probe itself
// errored) must fail open exactly like the pre-existing behaviour: launch
// rather than refuse over a check that could not be answered.
func TestOpenStudioLaunchesWhenCheckErrors(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeOpener{place: "C:\\place.rbxl"}
	a.server.studio = fake
	a.server.studioOpenCheck = func(context.Context, string) (StudioOpenCheck, error) {
		return StudioOpenCheck{}, errors.New("probe failed")
	}
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/open-studio", map[string]any{})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotPath == "" {
		t.Error("a failed check should fail open and still launch")
	}
}

// Creating a project with openStudio:true must apply the exact same
// no-duplicate-launch rule the manual "Open Studio" button applies: a Notice
// from the check means Studio is open elsewhere, so the launch must be
// withheld rather than unconditionally opened.
func TestCreateProjectWithOpenStudioWithholdsLaunchOnMismatch(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeOpener{place: "C:\\place.rbxl"}
	a.server.studio = fake
	notice := "Studio MCP withheld: the open Studio does not hold this project's place"
	a.server.studioOpenCheck = func(context.Context, string) (StudioOpenCheck, error) {
		return StudioOpenCheck{Notice: notice}, nil
	}
	projectPath := filepath.Join(t.TempDir(), "withheld-project")
	rec := postJSON(t, a, cookie, "/api/v1/projects", map[string]any{"name": "Withheld", "path": projectPath, "create": true, "openStudio": true})
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotPath != "" {
		t.Error("a mismatched Studio must not be relaunched by project creation")
	}
	var body struct {
		StudioNotice string `json:"studioNotice"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.StudioNotice != notice {
		t.Errorf("studioNotice=%q, want the withheld notice surfaced rather than swallowed", body.StudioNotice)
	}
}

// An instance already holding the new project's place is already there:
// creation must not relaunch it, and must not report a notice either, since
// nothing was withheld.
func TestCreateProjectWithOpenStudioSkipsRelaunchWhenAlreadyMatched(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeOpener{place: "C:\\place.rbxl"}
	a.server.studio = fake
	a.server.studioOpenCheck = func(context.Context, string) (StudioOpenCheck, error) {
		return StudioOpenCheck{Matched: true, Place: "C:\\already-open.rbxl"}, nil
	}
	projectPath := filepath.Join(t.TempDir(), "matched-project")
	rec := postJSON(t, a, cookie, "/api/v1/projects", map[string]any{"name": "Matched", "path": projectPath, "create": true, "openStudio": true})
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotPath != "" {
		t.Error("an already-open project's place must not be relaunched by project creation")
	}
	var body struct {
		StudioNotice string `json:"studioNotice"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.StudioNotice != "" {
		t.Errorf("studioNotice=%q, want empty since nothing was withheld", body.StudioNotice)
	}
}

// Nothing open at all is the safe case: project creation must still launch,
// same as when no studioOpenCheck is wired at all.
func TestCreateProjectWithOpenStudioLaunchesWhenCheckReportsNothingOpen(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeOpener{place: "C:\\place.rbxl"}
	a.server.studio = fake
	a.server.studioOpenCheck = func(context.Context, string) (StudioOpenCheck, error) {
		return StudioOpenCheck{Open: true}, nil
	}
	projectPath := filepath.Join(t.TempDir(), "open-project")
	rec := postJSON(t, a, cookie, "/api/v1/projects", map[string]any{"name": "OpenAway", "path": projectPath, "create": true, "openStudio": true})
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotPath == "" {
		t.Error("nothing open should still let project creation launch Studio")
	}
}

// A check that fails to run at all must fail open exactly like the manual
// button: launch rather than withhold over a check that could not be
// answered.
func TestCreateProjectWithOpenStudioLaunchesWhenCheckErrors(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	fake := &fakeOpener{place: "C:\\place.rbxl"}
	a.server.studio = fake
	a.server.studioOpenCheck = func(context.Context, string) (StudioOpenCheck, error) {
		return StudioOpenCheck{}, errors.New("probe failed")
	}
	projectPath := filepath.Join(t.TempDir(), "check-error-project")
	rec := postJSON(t, a, cookie, "/api/v1/projects", map[string]any{"name": "CheckErrors", "path": projectPath, "create": true, "openStudio": true})
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotPath == "" {
		t.Error("a failed check should fail open and still let project creation launch")
	}
}

// fakeRojoBuilder stands in for a real Rojo build so the handler-level
// dedup test below can drive a real *studio.Opener without touching Rojo.
type fakeRojoBuilder struct{}

func (fakeRojoBuilder) Build(_ context.Context, _, output string) error {
	return os.WriteFile(output, []byte("RBLX"), 0o600)
}
func (fakeRojoBuilder) InstallPlugin(context.Context) error { return nil }

// The manual button and a run's own auto-open funnel through the same
// *studio.Opener in production; this proves the sharing at the handler
// boundary — a second click while the first launch is still pending must not
// duplicate it, even with no studioOpenCheck wired at all.
func TestOpenStudioHandlerDoesNotDuplicateAPendingLaunch(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	var launches int32
	opener := &studio.Opener{
		Rojo:         fakeRojoBuilder{},
		DetectStudio: func() (string, error) { return "C:\\Studio.exe", nil },
		Launch: func(string, string) error {
			atomic.AddInt32(&launches, 1)
			return nil
		},
	}
	a.server.studio = opener
	rec1 := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/open-studio", map[string]any{})
	rec2 := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/open-studio", map[string]any{})
	if rec1.Code != 200 || rec2.Code != 200 {
		t.Fatalf("status1=%d body1=%s status2=%d body2=%s", rec1.Code, rec1.Body.String(), rec2.Code, rec2.Body.String())
	}
	if got := atomic.LoadInt32(&launches); got != 1 {
		t.Fatalf("launches=%d, want exactly 1 — a second click while the first is still pending must not relaunch Studio", got)
	}
}

// The badge asks about a project, not about Studio in general: "a Studio is
// open" and "this project's Studio is open" were indistinguishable while every
// project built to the same place file name.
func TestStudioStatusReportsStateRelativeToTheProject(t *testing.T) {
	for _, tc := range []struct {
		what   string
		status StudioStatus
		want   string
	}{
		{"this project's place is open", StudioStatus{Open: 1, Matched: 1}, "matched"},
		{"only another project's place is open", StudioStatus{Open: 2}, "other"},
		{"nothing is open", StudioStatus{}, "none"},
	} {
		a := newTestAPI(t)
		cookie := bootstrapCookie(t, a)
		a.server.studioStatus = func(context.Context, string) (StudioStatus, error) { return tc.status, nil }
		rec := getJSON(t, a, cookie, "/api/v1/studio-status?project=demo-obby")
		var body struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.State != tc.want {
			t.Errorf("%s: state=%q, want %q", tc.what, body.State, tc.want)
		}
	}
}

// The project asked about decides the answer, so the handler must pass it on.
func TestStudioStatusPassesTheProjectThrough(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	got := ""
	a.server.studioStatus = func(_ context.Context, projectID string) (StudioStatus, error) {
		got = projectID
		return StudioStatus{}, nil
	}
	getJSON(t, a, cookie, "/api/v1/studio-status?project=demo-obby")
	if got != "demo-obby" {
		t.Fatalf("projectID=%q", got)
	}
}

// A launcher that cannot be reached must not read as "no Studio is open" with
// no explanation.
func TestStudioStatusSurfacesAProbeFailure(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.studioStatus = func(context.Context, string) (StudioStatus, error) {
		return StudioStatus{}, errors.New("launcher unavailable")
	}
	rec := getJSON(t, a, cookie, "/api/v1/studio-status")
	if !strings.Contains(rec.Body.String(), "launcher unavailable") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

// Under --mock, or on any daemon that never wired a refresher, the endpoint
// must behave as a no-op that reflects whatever is already stored, not an
// error and not a claim that discovery ran.
func TestRefreshStudioSessionsWithNoRefresherWiredReportsDetected(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postJSON(t, a, cookie, "/api/v1/studio/sessions/refresh", map[string]any{})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Detected bool `json:"detected"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Detected {
		t.Error("with no refresher wired, detected must default true rather than reading as an absent launcher")
	}
}

func TestRefreshStudioSessionsCallsTheHookAndReportsDetection(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	called := false
	a.server.refreshStudioSessions = func(context.Context) (bool, error) {
		called = true
		return false, nil
	}
	rec := postJSON(t, a, cookie, "/api/v1/studio/sessions/refresh", map[string]any{})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Error("the refresher hook must be called")
	}
	var body struct {
		Detected bool `json:"detected"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Detected {
		t.Error("detected must reflect what the hook reported, not default true once a hook is wired")
	}
}

// A failed discovery pass (the launcher misbehaved mid-probe) must not fail
// the request - the operator still needs to see whatever is already stored.
func TestRefreshStudioSessionsSurfacesAHookErrorWithoutFailingTheRequest(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	a.server.refreshStudioSessions = func(context.Context) (bool, error) {
		return false, errors.New("launcher crashed mid-probe")
	}
	rec := postJSON(t, a, cookie, "/api/v1/studio/sessions/refresh", map[string]any{})
	if rec.Code != 200 {
		t.Fatalf("a refresh failure must not fail the request, status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "launcher crashed mid-probe") {
		t.Errorf("body=%s, want the failure explained", rec.Body.String())
	}
}
