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

	if h, b, err := Checkpoint(root, "x"); err != nil || h != "" || b != "" {
		t.Fatalf("a non-git project must be a silent no-op, got hash=%q branch=%q err=%v", h, b, err)
	}

	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Fatal(err)
	}
	if h, b, _ := Checkpoint(root, "empty"); h != "" || b != "" {
		t.Errorf("nothing to commit must not create a checkpoint, got hash=%q branch=%q", h, b)
	}

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, b, err := Checkpoint(root, "first change")
	if err != nil || h == "" || b == "" {
		t.Fatalf("a changed working tree must be checkpointed, got hash=%q branch=%q err=%v", h, b, err)
	}
}
