package agenttools

import (
	"strings"
	"testing"
)

// The workspace-write allowlist exists to keep an agent from running arbitrary
// programs. An allowlisted interpreter handed inline source text defeats that
// completely, so those forms are refused with the command allowlist itself.
func TestWorkspaceCommandRefusesInlineCodeExecution(t *testing.T) {
	cases := []struct {
		name string
		exe  string
		argv []string
	}{
		{"node eval short", "node", []string{"-e", "require('fs').rmSync('/', {recursive:true})"}},
		{"node eval long", "node", []string{"--eval", "1+1"}},
		{"node print", "node", []string{"-p", "process.env"}},
		{"python command", "python", []string{"-c", "import os; os.system('id')"}},
		{"python3 command", "python3", []string{"-c", "print(1)"}},
		{"go run", "go", []string{"run", "./evil"}},
		{"windows node", `C:\tools\node.exe`, []string{"-e", "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refusal := checkWorkspaceCommand(tc.exe, tc.argv)
			if refusal == "" {
				t.Fatalf("%s %v should be refused in workspace-write", tc.exe, tc.argv)
			}
			if !strings.Contains(refusal, "danger-full-access") {
				t.Fatalf("refusal should name the profile that allows it, got %q", refusal)
			}
		})
	}
}

func TestWorkspaceCommandRefusesNpx(t *testing.T) {
	if checkWorkspaceCommand("npx", []string{"some-package"}) == "" {
		t.Fatal("npx fetches and runs an arbitrary package and must not be allowlisted")
	}
}

func TestWorkspaceCommandAllowsOrdinaryDevelopmentCommands(t *testing.T) {
	cases := [][]string{
		{"git", "status"},
		{"go", "build", "./..."},
		{"go", "test", "./..."},
		{"npm", "install"},
		{"node", "scripts/build.js"},
		{"python", "tools/generate.py"},
		{"rojo", "build", "-o", "place.rbxl"},
	}
	for _, argv := range cases {
		if refusal := checkWorkspaceCommand(argv[0], argv[1:]); refusal != "" {
			t.Fatalf("%v should be allowed in workspace-write, got %q", argv, refusal)
		}
	}
}

func TestTruncateBytesNeverSplitsAUTF8Sequence(t *testing.T) {
	source := "аб" // two 2-byte runes
	if got := truncateBytes(source, 3); got != "а" {
		t.Fatalf("truncating mid-rune should drop the partial rune, got %q", got)
	}
	if got := truncateBytes(source, 4); got != source {
		t.Fatalf("a limit at a rune boundary should keep everything, got %q", got)
	}
	if got := truncateBytes(source, 0); got != "" {
		t.Fatalf("a zero limit should yield an empty string, got %q", got)
	}
}
