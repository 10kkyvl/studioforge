package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScaffoldWritesRojoSkeleton(t *testing.T) {
	root := t.TempDir()
	if err := Scaffold(root, "My Game"); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "default.project.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("default.project.json not written: %v", err)
	}
	for _, dir := range []string{filepath.Join(root, "src", "server"), filepath.Join(root, "src", "client")} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Fatalf("expected directory %s, err=%v", dir, err)
		}
	}
}

func TestScaffoldIsIdempotentAndPreservesExistingManifest(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "default.project.json")
	if err := os.WriteFile(manifest, []byte("custom"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Scaffold(root, "My Game"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "custom" {
		t.Errorf("scaffold must not overwrite an existing manifest, got %q", body)
	}
}
