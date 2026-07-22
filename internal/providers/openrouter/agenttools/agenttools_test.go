package agenttools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/gitops"
	"github.com/10kkyvl/studioforge/internal/processes"
)

func newTestToolSet(t *testing.T, profile Profile) (*ToolSet, string) {
	t.Helper()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Workspace: ws, Git: gitops.New(), ProjectID: "proj", RunID: "run"}
	if profile != ProfileReadOnly {
		sup := processes.NewSupervisor()
		t.Cleanup(func() { _ = sup.Close(context.Background()) })
		opts.Supervisor = sup
	}
	set, err := NewToolSet(profile, opts)
	if err != nil {
		t.Fatal(err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return set, realRoot
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWorkspaceRejectsPathTraversal(t *testing.T) {
	set, _ := newTestToolSet(t, ProfileReadOnly)
	for _, p := range []string{"../secret", "../../etc/passwd", "..\\..\\x"} {
		res := set.Execute(context.Background(), "read_file", mustJSON(t, map[string]string{"path": p}))
		if !res.IsError {
			t.Fatalf("path %q: expected IsError, got %+v", p, res)
		}
	}
}

func TestWorkspaceRejectsAbsolutePaths(t *testing.T) {
	set, _ := newTestToolSet(t, ProfileReadOnly)
	for _, p := range []string{"C:/Windows/win.ini", "/etc/hosts", `C:\Windows\win.ini`} {
		res := set.Execute(context.Background(), "read_file", mustJSON(t, map[string]string{"path": p}))
		if !res.IsError {
			t.Fatalf("path %q: expected IsError, got %+v", p, res)
		}
	}
}

func TestWorkspaceRejectsSymlinkEscape(t *testing.T) {
	set, root := newTestToolSet(t, ProfileReadOnly)
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("outside content"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outsideDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	res := set.Execute(context.Background(), "list_dir", mustJSON(t, map[string]string{"path": "escape"}))
	if !res.IsError {
		t.Fatalf("list_dir through symlink: expected IsError, got %+v", res)
	}
	res = set.Execute(context.Background(), "read_file", mustJSON(t, map[string]string{"path": "escape/secret.txt"}))
	if !res.IsError {
		t.Fatalf("read_file through symlink: expected IsError, got %+v", res)
	}
}

func TestReadFileTruncatesOversizedFile(t *testing.T) {
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("a", 1000)
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	set, err := NewToolSet(ProfileReadOnly, Options{Workspace: ws, Git: gitops.New(), MaxReadBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	res := set.Execute(context.Background(), "read_file", mustJSON(t, map[string]string{"path": "big.txt"}))
	if res.IsError {
		t.Fatalf("expected success with truncation note, got error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "truncated") {
		t.Fatalf("expected truncation note, got: %s", res.Content)
	}
}

func TestReadFileRejectsBinary(t *testing.T) {
	set, root := newTestToolSet(t, ProfileReadOnly)
	data := append([]byte("hello"), 0x00, 0x01, 0x02, 'w', 'o', 'r', 'l', 'd')
	if err := os.WriteFile(filepath.Join(root, "bin.dat"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	res := set.Execute(context.Background(), "read_file", mustJSON(t, map[string]string{"path": "bin.dat"}))
	if !res.IsError || !strings.Contains(strings.ToLower(res.Content), "binary") {
		t.Fatalf("expected binary IsError, got %+v", res)
	}
}

func TestCreateFileThenReadFileRoundTrips(t *testing.T) {
	set, _ := newTestToolSet(t, ProfileWorkspace)
	create := set.Execute(context.Background(), "create_file", mustJSON(t, map[string]string{"path": "sub/hello.txt", "content": "hello world"}))
	if create.IsError {
		t.Fatalf("create_file failed: %s", create.Content)
	}
	read := set.Execute(context.Background(), "read_file", mustJSON(t, map[string]string{"path": "sub/hello.txt"}))
	if read.IsError || read.Content != "hello world" {
		t.Fatalf("read_file mismatch: %+v", read)
	}
}

func TestReplaceExactTextUniqueNotFoundNotUnique(t *testing.T) {
	set, root := newTestToolSet(t, ProfileWorkspace)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("alpha beta gamma"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := set.Execute(context.Background(), "replace_exact_text", mustJSON(t, map[string]string{"path": "f.txt", "old_text": "beta", "new_text": "BETA"}))
	if res.IsError {
		t.Fatalf("unique replace failed: %s", res.Content)
	}
	data, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(data) != "alpha BETA gamma" {
		t.Fatalf("unexpected content: %s", data)
	}
	res = set.Execute(context.Background(), "replace_exact_text", mustJSON(t, map[string]string{"path": "f.txt", "old_text": "nope", "new_text": "x"}))
	if !res.IsError || !strings.Contains(res.Content, "not found") {
		t.Fatalf("expected not found error, got %+v", res)
	}
	if err := os.WriteFile(filepath.Join(root, "dup.txt"), []byte("aa aa"), 0o600); err != nil {
		t.Fatal(err)
	}
	res = set.Execute(context.Background(), "replace_exact_text", mustJSON(t, map[string]string{"path": "dup.txt", "old_text": "aa", "new_text": "b"}))
	if !res.IsError || !strings.Contains(res.Content, "not unique") {
		t.Fatalf("expected not unique error, got %+v", res)
	}
}

func TestApplyPatchAllOrNothing(t *testing.T) {
	set, root := newTestToolSet(t, ProfileWorkspace)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("hello b"), 0o600); err != nil {
		t.Fatal(err)
	}
	edits := []map[string]string{
		{"path": "a.txt", "old_text": "hello", "new_text": "HI"},
		{"path": "b.txt", "old_text": "does-not-exist", "new_text": "HI"},
	}
	res := set.Execute(context.Background(), "apply_patch", mustJSON(t, map[string]any{"edits": edits}))
	if !res.IsError {
		t.Fatalf("expected apply_patch failure, got %+v", res)
	}
	dataA, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	dataB, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(dataA) != "hello a" || string(dataB) != "hello b" {
		t.Fatalf("apply_patch mutated files despite failure: a=%q b=%q", dataA, dataB)
	}

	goodEdits := []map[string]string{
		{"path": "a.txt", "old_text": "hello", "new_text": "HI"},
		{"path": "b.txt", "old_text": "hello", "new_text": "HI"},
	}
	res = set.Execute(context.Background(), "apply_patch", mustJSON(t, map[string]any{"edits": goodEdits}))
	if res.IsError {
		t.Fatalf("expected apply_patch success, got %+v", res)
	}
	dataA, _ = os.ReadFile(filepath.Join(root, "a.txt"))
	dataB, _ = os.ReadFile(filepath.Join(root, "b.txt"))
	if string(dataA) != "HI a" || string(dataB) != "HI b" {
		t.Fatalf("apply_patch did not apply both edits: a=%q b=%q", dataA, dataB)
	}
}

func TestReplaceExactTextPreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits not supported on windows")
	}
	set, root := newTestToolSet(t, ProfileWorkspace)
	path := filepath.Join(root, "script.sh")
	if err := os.WriteFile(path, []byte("echo old"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := set.Execute(context.Background(), "replace_exact_text", mustJSON(t, map[string]string{"path": "script.sh", "old_text": "old", "new_text": "new"}))
	if res.IsError {
		t.Fatalf("replace_exact_text failed: %s", res.Content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected mode 0o755 to be preserved, got %o", info.Mode().Perm())
	}
}

func TestApplyPatchCaseInsensitiveMergesSamePath(t *testing.T) {
	set, root := newTestToolSet(t, ProfileWorkspace)
	if err := os.MkdirAll(filepath.Join(root, "Sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	upperPath := filepath.Join(root, "Sub", "File.txt")
	if err := os.WriteFile(upperPath, []byte("first second"), 0o600); err != nil {
		t.Fatal(err)
	}
	lowerRelPath := "sub/file.txt"
	if runtime.GOOS == "linux" {
		if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "sub", "file.txt"), []byte("first second"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	edits := []map[string]string{
		{"path": "Sub/File.txt", "old_text": "first", "new_text": "FIRST"},
		{"path": lowerRelPath, "old_text": "second", "new_text": "SECOND"},
	}
	res := set.Execute(context.Background(), "apply_patch", mustJSON(t, map[string]any{"edits": edits}))
	if res.IsError {
		t.Fatalf("expected apply_patch success, got %+v", res)
	}

	upperData, err := os.ReadFile(upperPath)
	if err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS == "linux" {
		if string(upperData) != "FIRST second" {
			t.Fatalf("expected only first edit applied to distinct-case file, got %q", upperData)
		}
		lowerData, err := os.ReadFile(filepath.Join(root, "sub", "file.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(lowerData) != "first SECOND" {
			t.Fatalf("expected only second edit applied to distinct-case file, got %q", lowerData)
		}
		return
	}

	if string(upperData) != "FIRST SECOND" {
		t.Fatalf("expected both edits merged into the same file, got %q", upperData)
	}
}

func TestListDirSearchFilesGrep(t *testing.T) {
	set, root := newTestToolSet(t, ProfileReadOnly)
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "one.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "two.go"), []byte("package sub\nfunc Needle() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("# hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := set.Execute(context.Background(), "list_dir", mustJSON(t, map[string]string{}))
	if res.IsError || !strings.Contains(res.Content, "one.go") || !strings.Contains(res.Content, "sub") {
		t.Fatalf("list_dir unexpected: %+v", res)
	}

	res = set.Execute(context.Background(), "search_files", mustJSON(t, map[string]string{"pattern": "*.go"}))
	if res.IsError || !strings.Contains(res.Content, "one.go") || !strings.Contains(res.Content, "two.go") {
		t.Fatalf("search_files unexpected: %+v", res)
	}

	res = set.Execute(context.Background(), "grep", mustJSON(t, map[string]string{"pattern": "Needle"}))
	if res.IsError || !strings.Contains(res.Content, "sub/two.go:2:") {
		t.Fatalf("grep unexpected: %+v", res)
	}

	res = set.Execute(context.Background(), "grep", mustJSON(t, map[string]string{"pattern": "((("}))
	if !res.IsError {
		t.Fatalf("expected grep to reject invalid regexp, got %+v", res)
	}
}

func TestProfilesGateTools(t *testing.T) {
	readOnly, _ := newTestToolSet(t, ProfileReadOnly)
	if readOnly.Has("create_file") || readOnly.Has("run_command") || readOnly.Has("apply_patch") {
		t.Fatalf("read-only profile should not expose write tools: %v", readOnly.Names())
	}
	if !readOnly.Has("read_file") || !readOnly.Has("git_status") {
		t.Fatalf("read-only profile missing expected tools: %v", readOnly.Names())
	}
	res := readOnly.Execute(context.Background(), "create_file", mustJSON(t, map[string]string{"path": "x", "content": "y"}))
	if !res.IsError || res.Content != "unknown tool: create_file" {
		t.Fatalf("expected unknown tool error, got %+v", res)
	}

	workspace, _ := newTestToolSet(t, ProfileWorkspace)
	if !workspace.Has("create_file") || !workspace.Has("run_command") {
		t.Fatalf("workspace-write profile missing expected tools: %v", workspace.Names())
	}

	danger, _ := newTestToolSet(t, ProfileDanger)
	if !danger.Has("run_command") {
		t.Fatalf("danger profile missing run_command: %v", danger.Names())
	}
}

func TestRunCommandAllowlistAndOutput(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not found on PATH")
	}
	_ = goPath
	set, _ := newTestToolSet(t, ProfileWorkspace)

	res := set.Execute(context.Background(), "run_command", mustJSON(t, map[string]any{"command": "go", "args": []string{"version"}}))
	if res.IsError {
		t.Fatalf("expected go version to succeed, got %+v", res)
	}
	if !strings.Contains(res.Content, "go version") {
		t.Fatalf("expected output to contain go version banner, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "[exit code 0]") {
		t.Fatalf("expected exit code marker, got: %s", res.Content)
	}

	res = set.Execute(context.Background(), "run_command", mustJSON(t, map[string]any{"command": "curl", "args": []string{"-s", "https://example.com"}}))
	if !res.IsError || !strings.Contains(res.Content, "not allowed in workspace-write profile") {
		t.Fatalf("expected disallowed command error, got %+v", res)
	}

	res = set.Execute(context.Background(), "run_command", mustJSON(t, map[string]any{"command": "go", "args": []string{"definitely-not-a-real-subcommand"}}))
	if !res.IsError || !strings.Contains(res.Content, "[exit code") {
		t.Fatalf("expected nonzero exit reported as error, got %+v", res)
	}

	smallSet, _ := newSmallOutputToolSet(t)
	res = smallSet.Execute(context.Background(), "run_command", mustJSON(t, map[string]any{"command": "go", "args": []string{"help"}}))
	if !strings.Contains(res.Content, "truncated") {
		t.Fatalf("expected MaxOutputBytes truncation note, got: %s", res.Content)
	}
}

func newSmallOutputToolSet(t *testing.T) (*ToolSet, string) {
	t.Helper()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	sup := processes.NewSupervisor()
	t.Cleanup(func() { _ = sup.Close(context.Background()) })
	set, err := NewToolSet(ProfileWorkspace, Options{Workspace: ws, Git: gitops.New(), Supervisor: sup, ProjectID: "proj", RunID: "small", MaxOutputBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	return set, root
}

func TestRunCommandShellDeniedOutsideDanger(t *testing.T) {
	set, _ := newTestToolSet(t, ProfileWorkspace)
	res := set.Execute(context.Background(), "run_command", mustJSON(t, map[string]any{"command": "echo hi", "shell": true}))
	if !res.IsError || !strings.Contains(res.Content, "danger-full-access") {
		t.Fatalf("expected shell denial, got %+v", res)
	}
}

func TestRunCommandDangerAllowsArbitraryExecutable(t *testing.T) {
	if _, err := exec.LookPath("hostname"); err != nil {
		t.Skip("hostname binary not found on PATH")
	}
	set, _ := newTestToolSet(t, ProfileDanger)
	res := set.Execute(context.Background(), "run_command", mustJSON(t, map[string]any{"command": "hostname"}))
	if res.IsError {
		t.Fatalf("expected danger profile to allow non-allowlisted executable, got %+v", res)
	}
}

func TestDefinitionsHaveValidSchemas(t *testing.T) {
	set, _ := newTestToolSet(t, ProfileDanger)
	defs := set.Definitions()
	if len(defs) != len(set.Names()) {
		t.Fatalf("Definitions() count %d != Names() count %d", len(defs), len(set.Names()))
	}
	for _, d := range defs {
		if d.Type != "function" {
			t.Fatalf("tool %s: unexpected type %q", d.Function.Name, d.Type)
		}
		if d.Function.Name == "" {
			t.Fatalf("tool has empty name: %+v", d)
		}
		if d.Function.Description == "" {
			t.Fatalf("tool %s has empty description", d.Function.Name)
		}
		if !json.Valid(d.Function.Parameters) {
			t.Fatalf("tool %s has invalid JSON schema: %s", d.Function.Name, d.Function.Parameters)
		}
	}
}
