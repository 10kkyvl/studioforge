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
func TestCheckpointIgnoresARepoRedirectingEnvironmentVariable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	t.Setenv("GIT_DIR", filepath.Join(elsewhere, "not-this-repo"))

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, b, err := Checkpoint(root, "checkpoint despite GIT_DIR")
	if err != nil || h == "" || b == "" {
		t.Fatalf("Checkpoint must commit root regardless of an inherited GIT_DIR, got hash=%q branch=%q err=%v", h, b, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "refs")); err != nil {
		t.Fatalf("commit must land in root's own .git, not the GIT_DIR override: %v", err)
	}
}
