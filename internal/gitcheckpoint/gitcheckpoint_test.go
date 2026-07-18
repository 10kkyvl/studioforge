package gitcheckpoint

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheckpoint(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()

	if h, err := Checkpoint(root, "x"); err != nil || h != "" {
		t.Fatalf("a non-git project must be a silent no-op, got hash=%q err=%v", h, err)
	}

	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Fatal(err)
	}
	if h, _ := Checkpoint(root, "empty"); h != "" {
		t.Errorf("nothing to commit must not create a checkpoint, got %q", h)
	}

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := Checkpoint(root, "first change")
	if err != nil || h == "" {
		t.Fatalf("a changed working tree must be checkpointed, got hash=%q err=%v", h, err)
	}
}
