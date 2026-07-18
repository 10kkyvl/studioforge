package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func TestSafeRollbackUsesNewBranchAndPreservesUntracked(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	file := filepath.Join(root, "game.lua")
	_ = os.WriteFile(file, []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	target := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(file, []byte("v2"), 0o600)
	git(t, root, "commit", "-am", "two")
	untracked := filepath.Join(root, "user-notes.txt")
	_ = os.WriteFile(untracked, []byte("keep"), 0o600)
	client := New()
	branch, err := client.SafeRollback(context.Background(), root, target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(branch, "studioforge/rollback-") {
		t.Fatalf("branch=%s", branch)
	}
	if _, err := os.Stat(untracked); err != nil {
		t.Fatalf("untracked file lost: %v", err)
	}
	head := git(t, root, "rev-parse", "HEAD")
	if head != target {
		t.Fatalf("head=%s target=%s", head, target)
	}
}
