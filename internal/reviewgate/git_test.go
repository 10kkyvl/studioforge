package reviewgate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/gitcheckpoint"
)

func gitRun(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
	return string(out)
}

func reviewRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "config", "core.autocrlf", "false")
	gitRun(t, root, "config", "user.name", "Review Test")
	gitRun(t, root, "config", "user.email", "review@example.invalid")
	if err := osWrite(filepath.Join(root, "tracked.txt"), "base\n"); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "tracked.txt")
	gitRun(t, root, "commit", "-qm", "initial")
	return root
}

func osWrite(path, value string) error {
	return os.WriteFile(path, []byte(value), 0o600)
}

func TestRejectRestoresTrackedAndRemovesOnlyRunCreatedFiles(t *testing.T) {
	root := reviewRepo(t)
	if err := osWrite(filepath.Join(root, "preexisting.txt"), "operator\n"); err != nil {
		t.Fatal(err)
	}
	checkpoint, _, err := gitcheckpoint.Checkpoint(root, "before review")
	if err != nil {
		t.Fatal(err)
	}
	if err := osWrite(filepath.Join(root, "tracked.txt"), "agent\n"); err != nil {
		t.Fatal(err)
	}
	if err := osWrite(filepath.Join(root, "created.txt"), "agent\n"); err != nil {
		t.Fatal(err)
	}
	if err := New().Reject(context.Background(), root, checkpoint); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(root, "tracked.txt"))); got != "base\n" {
		t.Fatalf("tracked=%q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "created.txt")); !os.IsNotExist(err) {
		t.Fatalf("run-created file still exists: %v", err)
	}
	if got := string(mustRead(t, filepath.Join(root, "preexisting.txt"))); got != "operator\n" {
		t.Fatalf("preexisting untracked=%q", got)
	}
}

func TestApplySelectedUsesGitPathspecAndKeepsOnlySelectedNewFiles(t *testing.T) {
	root := reviewRepo(t)
	if err := osWrite(filepath.Join(root, "baseline.txt"), "baseline\n"); err != nil {
		t.Fatal(err)
	}
	checkpoint, _, err := gitcheckpoint.Checkpoint(root, "before review")
	if err != nil {
		t.Fatal(err)
	}
	if err := osWrite(filepath.Join(root, "tracked.txt"), "selected\n"); err != nil {
		t.Fatal(err)
	}
	if err := osWrite(filepath.Join(root, "other.txt"), "drop\n"); err != nil {
		t.Fatal(err)
	}
	if err := osWrite(filepath.Join(root, "keep new.txt"), "keep\n"); err != nil {
		t.Fatal(err)
	}
	if err := osWrite(filepath.Join(root, "drop new.txt"), "drop\n"); err != nil {
		t.Fatal(err)
	}
	if err := New().ApplySelected(context.Background(), root, checkpoint, []string{"tracked.txt", "keep new.txt"}, []string{"keep new.txt", "drop new.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(root, "tracked.txt"))); got != "selected\n" {
		t.Fatalf("selected tracked=%q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "other.txt")); !os.IsNotExist(err) {
		t.Fatalf("unselected tracked file remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "drop new.txt")); !os.IsNotExist(err) {
		t.Fatalf("unselected new file remains: %v", err)
	}
	if got := string(mustRead(t, filepath.Join(root, "keep new.txt"))); got != "keep\n" {
		t.Fatalf("selected new=%q", got)
	}
}

func TestApplySelectedRejectsTraversalBeforeChangingTree(t *testing.T) {
	root := reviewRepo(t)
	checkpoint, _, err := gitcheckpoint.Checkpoint(root, "before review")
	if err != nil {
		t.Fatal(err)
	}
	if err := osWrite(filepath.Join(root, "tracked.txt"), "agent\n"); err != nil {
		t.Fatal(err)
	}
	err = New().ApplySelected(context.Background(), root, checkpoint, []string{"../tracked.txt"}, nil)
	if err == nil || !strings.Contains(err.Error(), "outside the project") {
		t.Fatalf("error=%v", err)
	}
	if got := string(mustRead(t, filepath.Join(root, "tracked.txt"))); got != "agent\n" {
		t.Fatalf("tree changed after rejected selection: %q", got)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestResolveRefusesOperatorChangesAndPreservesRecovery(t *testing.T) {
	root := reviewRepo(t)
	base := strings.TrimSpace(gitRun(t, root, "rev-parse", "HEAD"))
	osWrite(filepath.Join(root, "tracked.txt"), "agent\n")
	osWrite(filepath.Join(root, "new file.txt"), "agent new\n")
	c := New()
	snapshot, err := c.Diff(context.Background(), root, base)
	if err != nil {
		t.Fatal(err)
	}
	osWrite(filepath.Join(root, "operator.txt"), "operator\n")
	if err := c.Resolve(context.Background(), root, base, snapshot, "reject", nil, nil); err == nil || !strings.Contains(err.Error(), "review_tree_changed") {
		t.Fatalf("expected stale snapshot refusal: %v", err)
	}
	if got := string(mustRead(t, filepath.Join(root, "tracked.txt"))); got != "agent\n" {
		t.Fatal("agent files changed on refusal")
	}
	os.Remove(filepath.Join(root, "operator.txt"))
	if err := c.Resolve(context.Background(), root, base, snapshot, "reject", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(root, "tracked.txt"))); got != "base\n" {
		t.Fatal(got)
	}
	if _, err := os.Stat(filepath.Join(root, "new file.txt")); !os.IsNotExist(err) {
		t.Fatal("new file survived reject")
	}
	recovery := gitRun(t, root, "show", "refs/studioforge/review-recovery/"+snapshot.Tree+":new file.txt")
	if recovery != "agent new\n" {
		t.Fatal("recovery snapshot missing")
	}
}
func TestResolveSelectedHunkAndLiteralFileNames(t *testing.T) {
	root := reviewRepo(t)
	lines := []string{}
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	baseText := strings.Join(lines, "\n") + "\n"
	osWrite(filepath.Join(root, "tracked.txt"), baseText)
	osWrite(filepath.Join(root, "[literal].txt"), "base\n")
	osWrite(filepath.Join(root, "l.txt"), "base\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "long baseline")
	base := strings.TrimSpace(gitRun(t, root, "rev-parse", "HEAD"))
	lines[2] = "first changed"
	lines[26] = "last changed"
	osWrite(filepath.Join(root, "tracked.txt"), strings.Join(lines, "\n")+"\n")
	osWrite(filepath.Join(root, "[literal].txt"), "keep\n")
	osWrite(filepath.Join(root, "l.txt"), "drop\n")
	c := New()
	snapshot, err := c.Diff(context.Background(), root, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Resolve(context.Background(), root, base, snapshot, "apply_selected", []string{"[literal].txt"}, []HunkSelection{{Path: "tracked.txt", Index: 1}}); err != nil {
		t.Fatal(err)
	}
	got := string(mustRead(t, filepath.Join(root, "tracked.txt")))
	if strings.Contains(got, "first changed") || !strings.Contains(got, "last changed") {
		t.Fatalf("wrong hunks kept: %s", got)
	}
	if string(mustRead(t, filepath.Join(root, "[literal].txt"))) != "keep\n" || string(mustRead(t, filepath.Join(root, "l.txt"))) != "base\n" {
		t.Fatal("literal path selection wrong")
	}
}
func TestResolveEmptySelectionAndInvalidHunk(t *testing.T) {
	root := reviewRepo(t)
	base := strings.TrimSpace(gitRun(t, root, "rev-parse", "HEAD"))
	osWrite(filepath.Join(root, "tracked.txt"), "agent\n")
	c := New()
	s, err := c.Diff(context.Background(), root, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Resolve(context.Background(), root, base, s, "apply_selected", nil, []HunkSelection{{Path: "tracked.txt", Index: 4}}); err == nil {
		t.Fatal("invalid hunk accepted")
	}
	if string(mustRead(t, filepath.Join(root, "tracked.txt"))) != "agent\n" {
		t.Fatal("invalid hunk changed tree")
	}
	if err := c.Resolve(context.Background(), root, base, s, "apply_selected", nil, nil); err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, filepath.Join(root, "tracked.txt"))) != "base\n" {
		t.Fatal("empty selection kept all files")
	}
}
func TestResolveBinaryDeletionAndNewFiles(t *testing.T) {
	root := reviewRepo(t)
	os.WriteFile(filepath.Join(root, "binary.bin"), []byte{0, 1, 2}, 0o600)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "binary baseline")
	base := strings.TrimSpace(gitRun(t, root, "rev-parse", "HEAD"))
	os.WriteFile(filepath.Join(root, "binary.bin"), []byte{0, 9, 8}, 0o600)
	os.Remove(filepath.Join(root, "tracked.txt"))
	osWrite(filepath.Join(root, "new space.txt"), "keep\n")
	c := New()
	s, err := c.Diff(context.Background(), root, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Resolve(context.Background(), root, base, s, "apply_selected", []string{"tracked.txt", "new space.txt"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "tracked.txt")); !os.IsNotExist(err) {
		t.Fatal("selected deletion not kept")
	}
	if string(mustRead(t, filepath.Join(root, "binary.bin"))) != string([]byte{0, 1, 2}) {
		t.Fatal("binary not restored")
	}
	if string(mustRead(t, filepath.Join(root, "new space.txt"))) != "keep\n" {
		t.Fatal("new file not preserved")
	}
}

func TestRejectHonorsRepositoryCRLFConversion(t *testing.T) {
	root := reviewRepo(t)
	gitRun(t, root, "config", "core.autocrlf", "true")
	path := filepath.Join(root, "tracked.txt")
	if err := osWrite(path, "agent edit\r\n"); err != nil {
		t.Fatal(err)
	}
	client := New()
	snapshot, err := client.Diff(context.Background(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Resolve(context.Background(), root, "HEAD", snapshot, "reject", nil, nil); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "base\r\n" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	if got := strings.TrimSpace(gitRun(t, root, "status", "--porcelain")); got != "" {
		t.Fatalf("restored tree is dirty: %s", got)
	}
}
