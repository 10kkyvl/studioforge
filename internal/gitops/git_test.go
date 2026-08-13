package gitops

import (
	"context"
	"errors"
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
func TestDiffHeadShowsChangesSinceHead(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	file := filepath.Join(root, "game.lua")
	_ = os.WriteFile(file, []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	_ = os.WriteFile(file, []byte("v2"), 0o600)
	client := New()
	diff, err := client.DiffHead(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-v1") || !strings.Contains(diff, "+v2") {
		t.Fatalf("diff=%s", diff)
	}
}
func TestDiffHeadNotARepoIsEmpty(t *testing.T) {
	root := t.TempDir()
	client := New()
	diff, err := client.DiffHead(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Fatalf("diff=%s", diff)
	}
}
func TestDiffCommitShowsChangesSinceGivenCommit(t *testing.T) {
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
	_ = os.WriteFile(file, []byte("v3"), 0o600)
	client := New()
	diff, err := client.DiffCommit(context.Background(), root, target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-v1") || !strings.Contains(diff, "+v3") {
		t.Fatalf("diff=%s", diff)
	}
}
func TestDiffCommitAgainstNonexistentCommitErrors(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	_ = os.WriteFile(filepath.Join(root, "game.lua"), []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	client := New()
	if _, err := client.DiffCommit(context.Background(), root, "0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("expected an error diffing against a nonexistent commit")
	}
}
func TestDiffCommitNotARepoIsEmpty(t *testing.T) {
	root := t.TempDir()
	client := New()
	diff, err := client.DiffCommit(context.Background(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Fatalf("diff=%s", diff)
	}
}
func TestDiffRangeShowsChangesBetweenTwoCommits(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	file := filepath.Join(root, "game.lua")
	_ = os.WriteFile(file, []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	from := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(file, []byte("v2"), 0o600)
	git(t, root, "commit", "-am", "two")
	to := git(t, root, "rev-parse", "HEAD")
	client := New()
	diff, err := client.DiffRange(context.Background(), root, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-v1") || !strings.Contains(diff, "+v2") {
		t.Fatalf("diff=%s", diff)
	}
}
func TestDiffRangeToHEADWorks(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	file := filepath.Join(root, "game.lua")
	_ = os.WriteFile(file, []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	from := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(file, []byte("v2"), 0o600)
	git(t, root, "commit", "-am", "two")
	client := New()
	diff, err := client.DiffRange(context.Background(), root, from, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-v1") || !strings.Contains(diff, "+v2") {
		t.Fatalf("diff=%s", diff)
	}
}
func TestDiffRangeUnknownRefReturnsErrUnknownRef(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	_ = os.WriteFile(filepath.Join(root, "game.lua"), []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	client := New()
	if _, err := client.DiffRange(context.Background(), root, "0000000000000000000000000000000000000000", "HEAD"); !errors.Is(err, ErrUnknownRef) {
		t.Fatalf("err=%v, want ErrUnknownRef", err)
	}
}
func TestDiffRangeNotAncestorReturnsErrNotAncestor(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	file := filepath.Join(root, "game.lua")
	_ = os.WriteFile(file, []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	first := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(file, []byte("v2"), 0o600)
	git(t, root, "commit", "-am", "two")
	second := git(t, root, "rev-parse", "HEAD")
	client := New()
	if _, err := client.DiffRange(context.Background(), root, second, first); !errors.Is(err, ErrNotAncestor) {
		t.Fatalf("err=%v, want ErrNotAncestor", err)
	}
}
func TestDiffRangeNotARepoIsEmpty(t *testing.T) {
	root := t.TempDir()
	client := New()
	diff, err := client.DiffRange(context.Background(), root, "HEAD", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Fatalf("diff=%s", diff)
	}
}
func TestDiffHeadNoChangesIsEmpty(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	file := filepath.Join(root, "game.lua")
	_ = os.WriteFile(file, []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	client := New()
	diff, err := client.DiffHead(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Fatalf("diff=%s", diff)
	}
}
