package gitops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/gitcheckpoint"
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
func TestGitEnvironmentDropsCommandInjectingVariables(t *testing.T) {
	deniedKeys := []string{
		"GIT_EXTERNAL_DIFF", "GIT_SSH", "GIT_SSH_COMMAND", "GIT_PROXY_COMMAND",
		"GIT_EDITOR", "EDITOR", "VISUAL", "GIT_PAGER", "PAGER",
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_OBJECT_DIRECTORY",
		"GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_COUNT",
		"GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0",
		"LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH",
		"GIT_TRACE", "GIT_TRACE_PACK_ACCESS", "GIT_TRACE_SETUP",
	}
	for _, key := range deniedKeys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "attacker-controlled")
			env := gitcheckpoint.ScrubbedEnvironment()
			for _, entry := range env {
				if entry == key+"=attacker-controlled" {
					t.Fatalf("%s must be stripped from the git subprocess environment, found %q", key, entry)
				}
			}
		})
	}
}
func TestGitEnvironmentKeepsCredentialHelperSupport(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	env := gitcheckpoint.ScrubbedEnvironment()
	want := map[string]bool{"PATH": false, "SSH_AUTH_SOCK": false}
	if home := os.Getenv("HOME"); home != "" {
		want["HOME"] = false
	}
	if profile := os.Getenv("USERPROFILE"); profile != "" {
		want["USERPROFILE"] = false
	}
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, tracked := want[key]; tracked {
				want[key] = true
			}
		}
	}
	for key, seen := range want {
		if !seen {
			t.Fatalf("%s must survive scrubbing so credential helpers keep working", key)
		}
	}
}
func TestGitStatusStillWorksWithScrubbedEnvironment(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	file := filepath.Join(root, "game.lua")
	_ = os.WriteFile(file, []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	client := New()
	status, err := client.Status(context.Background(), root)
	if err != nil {
		t.Fatalf("Status with a scrubbed environment must still succeed: %v", err)
	}
	if !strings.HasPrefix(status, "##") {
		t.Fatalf("status=%q", status)
	}
}
func TestTagRefusesALeadingDash(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	_ = os.WriteFile(filepath.Join(root, "game.lua"), []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	client := New()
	if err := client.Tag(context.Background(), root, "-d"); !errors.Is(err, ErrInvalidTagName) {
		t.Fatalf("err=%v, want ErrInvalidTagName", err)
	}
	if out := git(t, root, "tag", "-l"); out != "" {
		t.Fatalf("tag -l=%q, want no tag created", out)
	}
}
func TestTagRefusesShellAndRefMetacharacters(t *testing.T) {
	cases := []string{
		"-d",
		"--file=/etc/passwd",
		"-F../../x",
		"a b",
		"a..b",
		"a.lock",
		"refs/",
		"..",
		"a\tb",
		"a\x01b",
	}
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	_ = os.WriteFile(filepath.Join(root, "game.lua"), []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	client := New()
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if err := client.Tag(context.Background(), root, name); !errors.Is(err, ErrInvalidTagName) {
				t.Fatalf("name=%q err=%v, want ErrInvalidTagName", name, err)
			}
		})
	}
	if out := git(t, root, "tag", "-l"); out != "" {
		t.Fatalf("tag -l=%q, want no tag created", out)
	}
}
func TestTagWritesTheNameAfterADoubleDash(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	_ = os.WriteFile(filepath.Join(root, "game.lua"), []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	client := New()
	if err := client.Tag(context.Background(), root, "milestone-1"); err != nil {
		t.Fatal(err)
	}
	out := git(t, root, "tag", "-l")
	if out != "milestone-1" {
		t.Fatalf("tag -l=%q, want milestone-1", out)
	}
}
func TestDiffRangeRefusesAnOptionShapedRef(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	_ = os.WriteFile(filepath.Join(root, "game.lua"), []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	client := New()
	if _, err := client.DiffRange(context.Background(), root, "--output=/tmp/pwned", "HEAD"); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("err=%v, want ErrInvalidRef", err)
	}
	if _, err := client.DiffRange(context.Background(), root, "HEAD", "--output=/tmp/pwned"); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("err=%v, want ErrInvalidRef", err)
	}
}
func TestSafeRollbackRefusesAnOptionShapedTarget(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "StudioForge Test")
	_ = os.WriteFile(filepath.Join(root, "game.lua"), []byte("v1"), 0o600)
	git(t, root, "add", "game.lua")
	git(t, root, "commit", "-m", "one")
	client := New()
	if _, err := client.SafeRollback(context.Background(), root, "--detach"); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("err=%v, want ErrInvalidRef", err)
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
