package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
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
