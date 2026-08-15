package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/10kkyvl/studioforge/internal/database"
)

func fakeStartupExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newStartupSettingsStore(t *testing.T) (*database.Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return database.NewStore(db), ctx
}

func TestStartupIgnoresAnInvalidToolPathFromTheDatabase(t *testing.T) {
	store, ctx := newStartupSettingsStore(t)
	if err := store.SetSetting(ctx, "claude_path", filepath.Join(t.TempDir(), "does-not-exist.exe")); err != nil {
		t.Fatal(err)
	}
	if got := validatedToolSetting(ctx, store, "claude_path"); got != "" {
		t.Fatalf("validatedToolSetting for an invalid stored path = %q, want empty", got)
	}
}

func TestStartupFallsBackToAutomaticSearchWhenNoToolPathIsStored(t *testing.T) {
	store, ctx := newStartupSettingsStore(t)
	if got := validatedToolSetting(ctx, store, "claude_path"); got != "" {
		t.Fatalf("validatedToolSetting for an unset setting = %q, want empty", got)
	}
}

func TestStartupAcceptsAValidStoredToolPath(t *testing.T) {
	store, ctx := newStartupSettingsStore(t)
	path := fakeStartupExecutable(t, "claude")
	if err := store.SetSetting(ctx, "claude_path", path); err != nil {
		t.Fatal(err)
	}
	got := validatedToolSetting(ctx, store, "claude_path")
	if got == "" {
		t.Fatal("validatedToolSetting for a valid stored path returned empty")
	}
	if filepath.Clean(got) != filepath.Clean(path) {
		t.Fatalf("validatedToolSetting = %q, want the normalized form of %q", got, path)
	}
}
