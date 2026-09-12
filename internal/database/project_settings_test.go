package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func TestProjectStyleDefaultsAndPersists(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, err := store.CreateProject(ctx, models.Project{
		Name:        "Styles",
		Path:        filepath.Join(t.TempDir(), "styles"),
		Fingerprint: "styles",
	})
	if err != nil {
		t.Fatal(err)
	}
	style, err := store.ProjectStyle(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if style != DefaultProjectStyle {
		t.Fatalf("default project style = %q, want %q", style, DefaultProjectStyle)
	}
	if err := store.SetProjectStyle(ctx, project.ID, "cyber"); err != nil {
		t.Fatal(err)
	}
	style, err = store.ProjectStyle(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if style != "cyber" {
		t.Fatalf("persisted project style = %q, want cyber", style)
	}
}
