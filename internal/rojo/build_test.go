package rojo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/10kkyvl/studioforge/internal/processes"
)

// TestBuildProducesPlace exercises the real rojo CLI. It skips when rojo is not
// installed so it stays green on machines without the toolchain.
func TestBuildProducesPlace(t *testing.T) {
	supervisor := processes.NewSupervisor()
	t.Cleanup(func() { supervisor.Close(context.Background()) })
	m := New(supervisor, "")
	if !m.Diagnose(context.Background()).Available {
		t.Skip("rojo CLI not installed")
	}
	dir := t.TempDir()
	projectFile := filepath.Join(dir, "default.project.json")
	if err := os.WriteFile(projectFile, []byte(`{"name":"t","tree":{"$className":"DataModel","ReplicatedStorage":{"$className":"ReplicatedStorage"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "place.rbxl")
	if err := m.Build(context.Background(), projectFile, out); err != nil {
		t.Fatalf("rojo build failed: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil || info.Size() == 0 {
		t.Fatalf("rojo build did not produce a non-empty place: err=%v", err)
	}
}
