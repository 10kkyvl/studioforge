package processes

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func skipIfUnconfinedAllowed(t *testing.T) {
	t.Helper()
	if allowUnconfined() {
		t.Skip("STUDIOFORGE_ALLOW_UNCONFINED=1 disables real OS confinement, which would make this test pass without exercising it; unset it to run this test for real")
	}
}

func lookToolOrSkip(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not found on PATH: %v", name, err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	return absolute
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runConfinedToolchainCommand(t *testing.T, spec Spec) {
	t.Helper()
	supervisor := NewSupervisor()
	t.Cleanup(func() { supervisor.Close(context.Background()) })
	process, err := supervisor.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("start %s: %v", spec.Kind, err)
	}
	var stdout, stderr strings.Builder
	for line := range process.Lines() {
		if line.Stream == "stderr" {
			stderr.WriteString(line.Text)
		} else {
			stdout.WriteString(line.Text)
		}
	}
	result := process.Wait()
	if result.ExitCode != 0 {
		t.Fatalf("%s exited %d, want 0\nstdout:\n%s\nstderr:\n%s", spec.Kind, result.ExitCode, stdout.String(), stderr.String())
	}
}

func goDirectiveVersion() string {
	version := strings.TrimPrefix(runtime.Version(), "go")
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return version
	}
	return parts[0] + "." + parts[1]
}

func TestConfinedNpmInstallSucceeds(t *testing.T) {
	skipIfUnconfinedAllowed(t)
	npm := lookToolOrSkip(t, "npm")

	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "package.json"), `{
  "name": "studioforge-confinement-fixture",
  "version": "1.0.0",
  "private": true,
  "dependencies": {
    "localdep": "file:./localdep"
  }
}
`)
	writeFixtureFile(t, filepath.Join(root, "localdep", "package.json"), `{
  "name": "localdep",
  "version": "1.0.0",
  "private": true
}
`)

	runConfinedToolchainCommand(t, Spec{
		ID:               "confined-npm-install",
		Kind:             "npm",
		Executable:       npm,
		Args:             []string{"install", "--no-audit", "--no-fund", "--offline", "--loglevel=error"},
		WorkingDirectory: root,
		Environment:      MinimalEnvironment(nil),
		MaxRuntime:       5 * time.Minute,
		Confine:          ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{root}},
	})
}

func TestConfinedGoTestSucceeds(t *testing.T) {
	skipIfUnconfinedAllowed(t)
	goTool := lookToolOrSkip(t, "go")

	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "go.mod"), "module example.test\n\ngo "+goDirectiveVersion()+"\n")
	writeFixtureFile(t, filepath.Join(root, "x.go"), `package example

func Add(a, b int) int { return a + b }
`)
	writeFixtureFile(t, filepath.Join(root, "x_test.go"), `package example

import "testing"

func TestAddReturnsSum(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
}
`)

	runConfinedToolchainCommand(t, Spec{
		ID:               "confined-go-test",
		Kind:             "go-test",
		Executable:       goTool,
		Args:             []string{"test", "./..."},
		WorkingDirectory: root,
		Environment:      MinimalEnvironment(nil),
		MaxRuntime:       2 * time.Minute,
		Confine:          ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{root}},
	})
}

func TestConfinedGoModDownloadSucceeds(t *testing.T) {
	skipIfUnconfinedAllowed(t)
	goTool := lookToolOrSkip(t, "go")

	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "go.mod"), "module example.download\n\ngo "+goDirectiveVersion()+"\n")

	runConfinedToolchainCommand(t, Spec{
		ID:               "confined-go-mod-download",
		Kind:             "go-mod-download",
		Executable:       goTool,
		Args:             []string{"mod", "download"},
		WorkingDirectory: root,
		Environment:      MinimalEnvironment(nil),
		MaxRuntime:       2 * time.Minute,
		Confine:          ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{root}},
	})
}

func TestConfinedRojoBuildSucceeds(t *testing.T) {
	skipIfUnconfinedAllowed(t)
	rojo := lookToolOrSkip(t, "rojo")

	root := t.TempDir()
	projectFile := filepath.Join(root, "default.project.json")
	writeFixtureFile(t, projectFile, `{
  "name": "ConfinedRojoBuildFixture",
  "tree": {
    "$className": "DataModel",
    "ServerScriptService": {"$path": "src"}
  }
}
`)
	writeFixtureFile(t, filepath.Join(root, "src", "init.server.lua"), `print("studioforge confinement fixture")
`)
	output := filepath.Join(root, "place.rbxl")

	runConfinedToolchainCommand(t, Spec{
		ID:               "confined-rojo-build",
		Kind:             "rojo-build",
		Executable:       rojo,
		Args:             []string{"build", projectFile, "--output", output},
		WorkingDirectory: root,
		Environment:      MinimalEnvironment(nil),
		MaxRuntime:       2 * time.Minute,
		Confine:          ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{root}},
	})

	info, err := os.Stat(output)
	if err != nil || info.Size() == 0 {
		t.Fatalf("rojo build did not produce a non-empty place file: err=%v", err)
	}
}
