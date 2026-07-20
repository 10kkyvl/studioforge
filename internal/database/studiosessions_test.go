package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func TestUpsertRealStudioSessionsInsertsNewInstances(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.UpsertRealStudioSessions(ctx, []models.StudioSession{
		{InstanceID: "inst-1", Name: "Obby-a1b2c3d4.rbxl", Active: true, PlayState: "edit"},
		{InstanceID: "inst-2", Name: "Tycoon-e5f6a7b8.rbxl", PlayState: "play"},
	}); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListStudioSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions=%d, want 2", len(sessions))
	}
	byInstance := map[string]models.StudioSession{}
	for _, s := range sessions {
		byInstance[s.InstanceID] = s
	}
	if got := byInstance["inst-1"]; got.Name != "Obby-a1b2c3d4.rbxl" || !got.Active || got.PlayState != "edit" || got.Mock {
		t.Errorf("inst-1=%+v", got)
	}
	if got := byInstance["inst-2"]; got.Name != "Tycoon-e5f6a7b8.rbxl" || got.PlayState != "play" || got.Mock {
		t.Errorf("inst-2=%+v", got)
	}
}

// A refresh must never silently undo an operator's own choice: once a session
// is bound, only an explicit BindStudio call may change it, not the next
// discovery pass — even one that resolved a different project for it.
func TestUpsertRealStudioSessionsPreservesAnExistingManualBinding(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, err := store.CreateProject(ctx, models.Project{Name: "Bound project", Path: filepath.Join(t.TempDir(), "bound-project"), Fingerprint: "bound-project"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRealStudioSessions(ctx, []models.StudioSession{{InstanceID: "inst-1", Name: "A.rbxl"}}); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListStudioSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions=%d, want 1", len(sessions))
	}
	if err := store.BindStudio(ctx, sessions[0].ID, project.ID); err != nil {
		t.Fatal(err)
	}

	other, err := store.CreateProject(ctx, models.Project{Name: "Other project", Path: filepath.Join(t.TempDir(), "other-project"), Fingerprint: "other-project"})
	if err != nil {
		t.Fatal(err)
	}
	// A later refresh resolves a *different* project for the same instance
	// (as an auto-matcher might if the operator renamed things) - the manual
	// binding must win regardless.
	if err := store.UpsertRealStudioSessions(ctx, []models.StudioSession{{InstanceID: "inst-1", Name: "A.rbxl", ProjectID: other.ID}}); err != nil {
		t.Fatal(err)
	}
	sessions, err = store.ListStudioSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ProjectID != project.ID {
		t.Fatalf("sessions=%+v, want the manual binding preserved", sessions)
	}
}

// A brand-new instance with no existing row has nothing to preserve, so the
// caller's own resolved match (e.g. by expected place name) takes effect.
func TestUpsertRealStudioSessionsAcceptsResolvedProjectForANewInstance(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, err := store.CreateProject(ctx, models.Project{Name: "Matched project", Path: filepath.Join(t.TempDir(), "matched-project"), Fingerprint: "matched-project"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRealStudioSessions(ctx, []models.StudioSession{{InstanceID: "inst-1", Name: "Matched-abcd1234.rbxl", ProjectID: project.ID}}); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListStudioSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ProjectID != project.ID {
		t.Fatalf("sessions=%+v, want the resolved project applied", sessions)
	}
}

func TestUpsertRealStudioSessionsRemovesInstancesNoLongerOpen(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.UpsertRealStudioSessions(ctx, []models.StudioSession{
		{InstanceID: "inst-1", Name: "A.rbxl"},
		{InstanceID: "inst-2", Name: "B.rbxl"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRealStudioSessions(ctx, []models.StudioSession{{InstanceID: "inst-1", Name: "A.rbxl"}}); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListStudioSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].InstanceID != "inst-1" {
		t.Fatalf("sessions=%+v, want only inst-1 left (inst-2's Studio closed)", sessions)
	}
}

// The mock demo's rows must survive a real discovery pass untouched, even one
// discovering nothing at all - --mock's rows are not this function's concern.
func TestUpsertRealStudioSessionsNeverTouchesMockRows(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	before, err := store.ListStudioSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("demo seed should have created mock studio sessions")
	}
	if err := store.UpsertRealStudioSessions(ctx, nil); err != nil {
		t.Fatal(err)
	}
	after, err := store.ListStudioSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("mock rows=%d after an empty real discovery pass, want %d untouched", len(after), len(before))
	}
	for _, s := range after {
		if !s.Mock {
			t.Fatalf("unexpected non-mock row after an empty discovery pass: %+v", s)
		}
	}
}
