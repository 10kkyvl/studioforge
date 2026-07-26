package agenttools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The allowlist matched on the basename alone, so any path ending in an
// allowlisted name passed it — and the value it had just "validated" went
// straight to exec.CommandContext. An agent under workspace-write may write
// files, which is the whole point of the profile, so the bypass was two steps:
// write git.cmd, run ./git.cmd.
func TestPathQualifiedCommandsAreRefused(t *testing.T) {
	for _, exe := range []string{
		"./git", ".\\git.exe", "../git", "tools/git", `tools\git.cmd`,
		"/usr/bin/git", `C:\Windows\System32\git.exe`, "C:git",
		`\\server\share\node.exe`, "./node", "sub/dir/../python",
	} {
		refusal := checkWorkspaceCommand(exe, nil)
		if refusal == "" {
			t.Errorf("checkWorkspaceCommand(%q) allowed a path-qualified executable", exe)
			continue
		}
		if !strings.Contains(refusal, "bare executable name") {
			t.Errorf("checkWorkspaceCommand(%q) refused for the wrong reason: %s", exe, refusal)
		}
	}
}

// The refusal above must be about paths, not about everything: a bare
// allowlisted name still passes the name check.
func TestBareAllowlistedNamesStillPassTheNameCheck(t *testing.T) {
	for _, exe := range []string{"git", "GIT", "git.exe", "npm", "node", "python3"} {
		if refusal := checkWorkspaceCommand(exe, nil); refusal != "" {
			t.Errorf("checkWorkspaceCommand(%q) = %q, want it allowed", exe, refusal)
		}
	}
}

func TestResolveWorkspaceCommandRefusesAnExecutableInsideTheProject(t *testing.T) {
	root := t.TempDir()
	// A tool the agent wrote for itself, under a name the allowlist trusts.
	name := "git"
	if runtime.GOOS == "windows" {
		name = "git.exe"
	}
	planted := filepath.Join(root, name)
	if err := os.WriteFile(planted, []byte("#!/bin/sh\necho pwned\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Put the project itself on PATH, which is the only way LookPath would find
	// the planted file — and exactly the case the check exists for.
	t.Setenv("PATH", root)
	resolved, refusal := resolveWorkspaceCommand("git", root)
	if refusal == "" {
		t.Fatalf("resolveWorkspaceCommand resolved to %q inside the project, want a refusal", resolved)
	}
	if !strings.Contains(refusal, "inside the project") {
		t.Errorf("refused for the wrong reason: %s", refusal)
	}
}

func TestResolveWorkspaceCommandReportsAMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, refusal := resolveWorkspaceCommand("git", t.TempDir()); !strings.Contains(refusal, "not found on PATH") {
		t.Errorf("refusal = %q, want it to name the missing tool", refusal)
	}
}

// A real tool outside the project resolves to an absolute path, which is what
// gets executed — the supervisor does no checking of its own.
func TestResolveWorkspaceCommandReturnsAnAbsolutePathOutsideTheProject(t *testing.T) {
	resolved, refusal := resolveWorkspaceCommand(goToolName(), t.TempDir())
	if refusal != "" {
		t.Skipf("the Go toolchain is not on PATH in this environment: %s", refusal)
	}
	if !filepath.IsAbs(resolved) {
		t.Errorf("resolved = %q, want an absolute path", resolved)
	}
}

func goToolName() string { return "go" }

// End to end through the tool, which is what actually protects a run: the
// two-step bypass must fail at the second step.
func TestRunCommandRefusesTheWrittenThenRunBypass(t *testing.T) {
	set, root := newTestToolSet(t, ProfileWorkspace)

	name := "git.cmd"
	if runtime.GOOS != "windows" {
		name = "git"
	}
	if err := os.WriteFile(filepath.Join(root, name), []byte("echo pwned\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"./" + name, ".\\" + name, filepath.Join(root, name)} {
		args, _ := json.Marshal(map[string]any{"command": command})
		res := set.Execute(context.Background(), "run_command", args)
		if !res.IsError {
			t.Fatalf("run_command(%q) succeeded; the allowlist was bypassed", command)
		}
		if strings.Contains(res.Content, "pwned") {
			t.Fatalf("run_command(%q) executed the planted file", command)
		}
	}
}

// danger-full-access is explicitly the profile for arbitrary commands, so none
// of the above applies to it.
func TestDangerProfileStillRunsAPathQualifiedCommand(t *testing.T) {
	set, _ := newTestToolSet(t, ProfileDanger)
	args, _ := json.Marshal(map[string]any{"command": "definitely-not-a-real-tool-xyz"})
	res := set.Execute(context.Background(), "run_command", args)
	// It fails because the tool does not exist, not because a profile refused
	// it — the refusal messages this change adds must not appear here.
	if strings.Contains(res.Content, "bare executable name") || strings.Contains(res.Content, "inside the project") {
		t.Errorf("danger-full-access was subjected to the workspace-write checks: %s", res.Content)
	}
}
