package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/gitops/diffparse"
)

func initRollbackRepo(t *testing.T, root string) {
	t.Helper()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	git(t, root, "config", "core.autocrlf", "false")
}

func TestSelectiveRollbackRevertsOneOfTwoModifiedFiles(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	fileA := filepath.Join(root, "a.lua")
	fileB := filepath.Join(root, "b.lua")
	_ = os.WriteFile(fileA, []byte("a1\n"), 0o600)
	_ = os.WriteFile(fileB, []byte("b1\n"), 0o600)
	git(t, root, "add", "a.lua", "b.lua")
	git(t, root, "commit", "-m", "one")
	checkpoint := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(fileA, []byte("a2\n"), 0o600)
	_ = os.WriteFile(fileB, []byte("b2\n"), 0o600)

	client := New()
	result, err := client.SelectiveRollback(context.Background(), root, checkpoint, "", []string{"a.lua"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.RevertedFiles != 1 || result.SafetyCommit == "" {
		t.Fatalf("result=%+v", result)
	}
	gotA, _ := os.ReadFile(fileA)
	gotB, _ := os.ReadFile(fileB)
	if string(gotA) != "a1\n" {
		t.Fatalf("a.lua=%q, want reverted to a1", gotA)
	}
	if string(gotB) != "b2\n" {
		t.Fatalf("b.lua=%q, want untouched at b2", gotB)
	}
}

func TestSelectiveRollbackRevertsRunAddedFile(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	_ = os.WriteFile(filepath.Join(root, "keep.lua"), []byte("v1\n"), 0o600)
	git(t, root, "add", "keep.lua")
	git(t, root, "commit", "-m", "one")
	checkpoint := git(t, root, "rev-parse", "HEAD")
	newFile := filepath.Join(root, "new.lua")
	_ = os.WriteFile(newFile, []byte("new\n"), 0o600)
	git(t, root, "add", "new.lua")

	client := New()
	result, err := client.SelectiveRollback(context.Background(), root, checkpoint, "", []string{"new.lua"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.RevertedFiles != 1 {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Fatalf("new.lua should be gone from disk, stat err=%v", err)
	}
	tracked := git(t, root, "ls-files", "new.lua")
	if tracked != "" {
		t.Fatalf("new.lua should not be tracked anymore, got %q", tracked)
	}
}

func TestSelectiveRollbackRevertsOneOfTwoHunksInOneFile(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	file := filepath.Join(root, "game.lua")
	original := "local a = 1\nlocal b = 2\nlocal c = 3\nlocal d = 4\nlocal e = 5\nlocal f = 6\nlocal g = 7\nlocal h = 8\nlocal i = 9\nlocal j = 10\n"
	_ = os.WriteFile(file, []byte(original), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	checkpoint := git(t, root, "rev-parse", "HEAD")
	changed := "local a = 100\nlocal b = 2\nlocal c = 3\nlocal d = 4\nlocal e = 5\nlocal f = 6\nlocal g = 7\nlocal h = 8\nlocal i = 9\nlocal j = 200\n"
	_ = os.WriteFile(file, []byte(changed), 0o600)

	client := New()
	result, err := client.SelectiveRollback(context.Background(), root, checkpoint, "", nil, []HunkSelection{{Path: "game.lua", Index: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if result.RevertedHunks != 1 {
		t.Fatalf("result=%+v", result)
	}
	got, _ := os.ReadFile(file)
	lines := strings.Split(string(got), "\n")
	if lines[0] != "local a = 1" {
		t.Fatalf("first hunk should be reverted, got %q", lines[0])
	}
	if lines[9] != "local j = 200" {
		t.Fatalf("second hunk should be intact, got %q", lines[9])
	}
}

func TestSelectiveRollbackRevertsEveryFileWhenGivenTheWholeDiff(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	fileA := filepath.Join(root, "a.lua")
	fileB := filepath.Join(root, "b.lua")
	_ = os.WriteFile(fileA, []byte("a1\n"), 0o600)
	_ = os.WriteFile(fileB, []byte("b1\n"), 0o600)
	git(t, root, "add", "a.lua", "b.lua")
	git(t, root, "commit", "-m", "one")
	checkpoint := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(fileA, []byte("a2\n"), 0o600)
	_ = os.WriteFile(fileB, []byte("b2\n"), 0o600)
	newFile := filepath.Join(root, "new.lua")
	_ = os.WriteFile(newFile, []byte("new\n"), 0o600)
	git(t, root, "add", "new.lua")

	client := New()
	rawDiff, err := client.DiffCommit(context.Background(), root, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	parsed := diffparse.Parse(rawDiff)
	files := make([]string, 0, len(parsed.Files))
	for _, f := range parsed.Files {
		files = append(files, f.Path)
	}
	result, err := client.SelectiveRollback(context.Background(), root, checkpoint, "", files, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.RevertedFiles != 3 {
		t.Fatalf("result=%+v, want all 3 changed files reverted", result)
	}
	gotA, _ := os.ReadFile(fileA)
	gotB, _ := os.ReadFile(fileB)
	if string(gotA) != "a1\n" || string(gotB) != "b1\n" {
		t.Fatalf("a.lua=%q b.lua=%q, want both reverted to their checkpoint content", gotA, gotB)
	}
	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Fatalf("new.lua should be gone from disk, stat err=%v", err)
	}
}

func TestSelectiveRollbackOnNonLatestRunViaCheckpointRange(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	fileA := filepath.Join(root, "a.lua")
	fileB := filepath.Join(root, "b.lua")
	_ = os.WriteFile(fileA, []byte("a1\n"), 0o600)
	_ = os.WriteFile(fileB, []byte("b1\n"), 0o600)
	git(t, root, "add", "a.lua", "b.lua")
	git(t, root, "commit", "-m", "base")
	c1 := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(fileA, []byte("a2\n"), 0o600)
	git(t, root, "commit", "-am", "edit a")
	c2 := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(fileB, []byte("b2\n"), 0o600)
	git(t, root, "commit", "-am", "edit b")

	client := New()
	result, err := client.SelectiveRollback(context.Background(), root, c1, c2, []string{"a.lua"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.RevertedFiles != 1 {
		t.Fatalf("result=%+v", result)
	}
	gotA, _ := os.ReadFile(fileA)
	gotB, _ := os.ReadFile(fileB)
	if string(gotA) != "a1\n" {
		t.Fatalf("a.lua=%q, want reverted to a1", gotA)
	}
	if string(gotB) != "b2\n" {
		t.Fatalf("b.lua=%q, want the later edit intact", gotB)
	}
}

func TestSelectiveRollbackErrLaterChangesWhenLaterCheckpointTouchesSelectedPath(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	file := filepath.Join(root, "a.lua")
	_ = os.WriteFile(file, []byte("a1\n"), 0o600)
	git(t, root, "add", "a.lua")
	git(t, root, "commit", "-m", "base")
	c1 := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(file, []byte("a2\n"), 0o600)
	git(t, root, "commit", "-am", "edit a again")
	c2 := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(file, []byte("a3\n"), 0o600)
	git(t, root, "commit", "-am", "edit a yet again")

	client := New()
	if _, err := client.SelectiveRollback(context.Background(), root, c1, c2, []string{"a.lua"}, nil); !errors.Is(err, ErrLaterChanges) {
		t.Fatalf("err=%v, want ErrLaterChanges", err)
	}
}

func TestSelectiveRollbackErrDirtyWorktreeOnRealMergeConflict(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	file := filepath.Join(root, "a.lua")
	_ = os.WriteFile(file, []byte("base\n"), 0o600)
	git(t, root, "add", "a.lua")
	git(t, root, "commit", "-m", "base")
	checkpoint := git(t, root, "rev-parse", "HEAD")
	git(t, root, "checkout", "-b", "side")
	_ = os.WriteFile(file, []byte("side\n"), 0o600)
	git(t, root, "commit", "-am", "side change")
	git(t, root, "checkout", "master")
	_ = os.WriteFile(file, []byte("main\n"), 0o600)
	git(t, root, "commit", "-am", "main change")
	_, _ = New().run(context.Background(), root, "merge", "side")

	client := New()
	if _, err := client.SelectiveRollback(context.Background(), root, checkpoint, "", []string{"a.lua"}, nil); !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("err=%v, want ErrDirtyWorktree", err)
	}
}

func TestSelectiveRollbackErrSelectionUnknownForBogusPath(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	file := filepath.Join(root, "a.lua")
	_ = os.WriteFile(file, []byte("a1\n"), 0o600)
	git(t, root, "add", "a.lua")
	git(t, root, "commit", "-m", "one")
	checkpoint := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(file, []byte("a2\n"), 0o600)

	client := New()
	if _, err := client.SelectiveRollback(context.Background(), root, checkpoint, "", []string{"missing.lua"}, nil); !errors.Is(err, ErrSelectionUnknown) {
		t.Fatalf("err=%v, want ErrSelectionUnknown", err)
	}
}

func TestSelectiveRollbackErrSelectionUnknownForOutOfRangeHunkIndex(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	file := filepath.Join(root, "a.lua")
	_ = os.WriteFile(file, []byte("a1\n"), 0o600)
	git(t, root, "add", "a.lua")
	git(t, root, "commit", "-m", "one")
	checkpoint := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(file, []byte("a2\n"), 0o600)

	client := New()
	if _, err := client.SelectiveRollback(context.Background(), root, checkpoint, "", nil, []HunkSelection{{Path: "a.lua", Index: 5}}); !errors.Is(err, ErrSelectionUnknown) {
		t.Fatalf("err=%v, want ErrSelectionUnknown", err)
	}
}

func TestSelectiveRollbackErrSelectionUnknownForSamePathInBothLists(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	file := filepath.Join(root, "a.lua")
	_ = os.WriteFile(file, []byte("a1\n"), 0o600)
	git(t, root, "add", "a.lua")
	git(t, root, "commit", "-m", "one")
	checkpoint := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(file, []byte("a2\n"), 0o600)

	client := New()
	if _, err := client.SelectiveRollback(context.Background(), root, checkpoint, "", []string{"a.lua"}, []HunkSelection{{Path: "a.lua", Index: 0}}); !errors.Is(err, ErrSelectionUnknown) {
		t.Fatalf("err=%v, want ErrSelectionUnknown", err)
	}
}

func TestSelectiveRollbackErrSelectionUnknownForBinaryFile(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	binFile := filepath.Join(root, "icon.png")
	_ = os.WriteFile(binFile, []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x01, 0x02}, 0o600)
	git(t, root, "add", "icon.png")
	git(t, root, "commit", "-m", "one")
	checkpoint := git(t, root, "rev-parse", "HEAD")
	_ = os.WriteFile(binFile, []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x01, 0x03}, 0o600)

	client := New()
	if _, err := client.SelectiveRollback(context.Background(), root, checkpoint, "", []string{"icon.png"}, nil); !errors.Is(err, ErrSelectionUnknown) {
		t.Fatalf("err=%v, want ErrSelectionUnknown", err)
	}
}

func TestSelectiveRollbackErrPatchCheckFailed(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	file := filepath.Join(root, "a.lua")
	_ = os.WriteFile(file, []byte("a1\n"), 0o600)
	git(t, root, "add", "a.lua")
	git(t, root, "commit", "-m", "one")

	badPatch := "diff --git a/a.lua b/a.lua\n--- a/a.lua\n+++ b/a.lua\n@@ -1,1 +1,1 @@\n-a1\n+does not match the worktree\n"
	client := New()
	if _, err := client.runStdin(context.Background(), root, badPatch, "apply", "--check", "-R", "-"); err == nil {
		t.Fatal("expected git apply --check -R to fail against a patch that does not describe the worktree")
	}
}

func TestSelectiveRollbackSafetyCommitCreatedAndReturned(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	file := filepath.Join(root, "a.lua")
	_ = os.WriteFile(file, []byte("a1\n"), 0o600)
	git(t, root, "add", "a.lua")
	git(t, root, "commit", "-m", "one")
	checkpoint := git(t, root, "rev-parse", "HEAD")
	before := git(t, root, "rev-list", "--count", "HEAD")
	_ = os.WriteFile(file, []byte("a2\n"), 0o600)

	client := New()
	result, err := client.SelectiveRollback(context.Background(), root, checkpoint, "", []string{"a.lua"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.SafetyCommit == "" {
		t.Fatal("expected a non-empty safety commit hash")
	}
	after := git(t, root, "rev-list", "--count", "HEAD")
	beforeN, _ := strconv.Atoi(before)
	afterN, _ := strconv.Atoi(after)
	if afterN != beforeN+1 {
		t.Fatalf("commit count before=%s after=%s, want +1 for the safety commit", before, after)
	}
	head := git(t, root, "rev-parse", "HEAD")
	if head != result.SafetyCommit {
		t.Fatalf("HEAD=%s, want the safety commit %s", head, result.SafetyCommit)
	}
}

func TestSelectiveRollbackRefusesAnOptionShapedCheckpoint(t *testing.T) {
	root := t.TempDir()
	initRollbackRepo(t, root)
	file := filepath.Join(root, "a.lua")
	_ = os.WriteFile(file, []byte("a1\n"), 0o600)
	git(t, root, "add", "a.lua")
	git(t, root, "commit", "-m", "one")

	client := New()
	if _, err := client.SelectiveRollback(context.Background(), root, "--output=/tmp/pwned", "", []string{"a.lua"}, nil); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("err=%v, want ErrInvalidRef", err)
	}
	checkpoint := git(t, root, "rev-parse", "HEAD")
	if _, err := client.SelectiveRollback(context.Background(), root, checkpoint, "--output=/tmp/pwned", []string{"a.lua"}, nil); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("err=%v, want ErrInvalidRef", err)
	}
}
func TestSelectiveRollbackErrNotGitRepoOnPlainDirectory(t *testing.T) {
	root := t.TempDir()
	client := New()
	if _, err := client.SelectiveRollback(context.Background(), root, "deadbeef", "", []string{"a.lua"}, nil); !errors.Is(err, ErrNotGitRepo) {
		t.Fatalf("err=%v, want ErrNotGitRepo", err)
	}
}
