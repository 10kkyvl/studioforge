package portable

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/10kkyvl/studioforge/internal/database"
)

func TestExportPreviewAndConflict(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := database.NewStore(db)
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "project.zip")
	if err := Export(ctx, store, "demo-obby", target); err != nil {
		t.Fatal(err)
	}
	manifest, err := Read(target)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.IncludesSource || manifest.Project.ID != "demo-obby" || len(manifest.Agents) != 3 {
		t.Fatalf("manifest=%+v", manifest)
	}
	preview, err := Inspect(ctx, store, target)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.PathConflict || !preview.NameConflict {
		t.Fatalf("preview=%+v", preview)
	}
}
