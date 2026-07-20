package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
)

func TestResolveSessionProjectsMatchesByExpectedPlaceName(t *testing.T) {
	projects := []models.Project{
		{ID: "a1b2c3d4-0000-0000-0000-000000000000", Name: "Skyline Obby"},
		{ID: "e5f6a7b8-0000-0000-0000-000000000000", Name: "Harbor Tycoon"},
	}
	instances := []mcp.Session{
		{InstanceID: "one", Name: "skyline-obby-a1b2c3d4.rbxl", Active: true, PlayState: "edit"},
		{InstanceID: "two", Name: "some-unrelated-place.rbxl", PlayState: "play"},
	}
	sessions := resolveSessionProjects(instances, projects)
	if len(sessions) != 2 {
		t.Fatalf("sessions=%d, want 2", len(sessions))
	}
	byInstance := map[string]models.StudioSession{}
	for _, s := range sessions {
		byInstance[s.InstanceID] = s
	}
	if got := byInstance["one"]; got.ProjectID != projects[0].ID || !got.Active || got.PlayState != "edit" {
		t.Errorf("instance one=%+v, want matched to %s", got, projects[0].ID)
	}
	if got := byInstance["two"]; got.ProjectID != "" {
		t.Errorf("instance two=%+v, want unmatched (empty project)", got)
	}
}

func TestResolveSessionProjectsWithNoProjectsLeavesEveryInstanceUnmatched(t *testing.T) {
	sessions := resolveSessionProjects([]mcp.Session{{InstanceID: "one", Name: "A.rbxl"}}, nil)
	if len(sessions) != 1 || sessions[0].ProjectID != "" {
		t.Fatalf("sessions=%+v, want one unmatched session", sessions)
	}
}

func TestResolveSessionProjectsWithNoInstancesReturnsNone(t *testing.T) {
	sessions := resolveSessionProjects(nil, []models.Project{{ID: "p1", Name: "Anything"}})
	if len(sessions) != 0 {
		t.Fatalf("sessions=%+v, want none", sessions)
	}
}

// fakeStudioSessionStore is a minimal in-memory studioSessionStore, so the
// refresher's own glue (does it call ListProjects, resolve, then persist) can
// be tested without a real database.
type fakeStudioSessionStore struct {
	projects    []models.Project
	upserted    []models.StudioSession
	upsertCalls int
	listErr     error
	upsertErr   error
}

func (f *fakeStudioSessionStore) ListProjects(context.Context, bool) ([]models.Project, error) {
	return f.projects, f.listErr
}
func (f *fakeStudioSessionStore) UpsertRealStudioSessions(_ context.Context, sessions []models.StudioSession) error {
	f.upsertCalls++
	f.upserted = sessions
	return f.upsertErr
}

// An absent launcher must report Detected=false without touching the store at
// all - there is nothing real to persist.
func TestStudioSessionsRefresherWhenLauncherIsAbsentLeavesTheStoreUntouched(t *testing.T) {
	dir := t.TempDir()
	provisioner := &mcp.Provisioner{Dir: dir, Override: func() string { return filepath.Join(dir, "missing-launcher") }}
	store := &fakeStudioSessionStore{}
	refresh := studioSessionsRefresher(provisioner, store)
	detected, err := refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if detected {
		t.Error("an absent launcher must report Detected=false")
	}
	if store.upsertCalls != 0 {
		t.Errorf("upsertCalls=%d, want 0 when nothing was discovered", store.upsertCalls)
	}
}

// fakeStudiosTransport answers list_roblox_studios with a fixed instance list
// for the refresher's happy-path wiring test.
type fakeStudiosTransport struct{ instances []mcp.Instance }

func (f *fakeStudiosTransport) ListTools(context.Context) ([]mcp.Tool, error) {
	return []mcp.Tool{{Name: "list_roblox_studios"}}, nil
}
func (f *fakeStudiosTransport) Call(_ context.Context, name string, _ map[string]any) (json.RawMessage, error) {
	if name != "list_roblox_studios" {
		return json.RawMessage(`{"content":[{"type":"text","text":"{}"}]}`), nil
	}
	listing, err := json.Marshal(map[string]any{"studios": f.instances})
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": string(listing)}}})
	if err != nil {
		return nil, err
	}
	return body, nil
}
func (f *fakeStudiosTransport) Close() error { return nil }

func TestStudioSessionsRefresherPersistsDiscoveredInstancesMatchedToProjects(t *testing.T) {
	dir := t.TempDir()
	launcher := filepath.Join(dir, "mcp-launcher")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	transport := &fakeStudiosTransport{instances: []mcp.Instance{{ID: "one", Name: "skyline-obby-a1b2c3d4.rbxl"}}}
	provisioner := &mcp.Provisioner{
		Dir:      dir,
		Override: func() string { return launcher },
		Dial:     func(context.Context, mcp.LaunchConfig) (mcp.Transport, error) { return transport, nil },
	}
	store := &fakeStudioSessionStore{projects: []models.Project{{ID: "a1b2c3d4-0000-0000-0000-000000000000", Name: "Skyline Obby"}}}
	refresh := studioSessionsRefresher(provisioner, store)
	detected, err := refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !detected {
		t.Error("a reachable launcher must report Detected=true")
	}
	if store.upsertCalls != 1 {
		t.Fatalf("upsertCalls=%d, want 1", store.upsertCalls)
	}
	if len(store.upserted) != 1 || store.upserted[0].ProjectID != "a1b2c3d4-0000-0000-0000-000000000000" {
		t.Fatalf("upserted=%+v, want the instance matched to the project", store.upserted)
	}
}
