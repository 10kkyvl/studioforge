//go:build darwin

package processes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSandboxAllowsWriteInsideRoot(t *testing.T) {
	root := t.TempDir()
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())

	script := fmt.Sprintf(`echo hi > %q`, filepath.Join(root, "inside.txt"))
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "sandbox-write-inside",
		Kind:        "test",
		Executable:  "/bin/sh",
		Args:        []string{"-c", script},
		Environment: MinimalEnvironment(nil),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := process.Wait()
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", result.ExitCode)
	}
	if _, err := os.Stat(filepath.Join(root, "inside.txt")); err != nil {
		t.Fatalf("expected inside.txt to exist: %v", err)
	}
}

func TestSandboxRefusesWriteOutsideRoot(t *testing.T) {
	root := t.TempDir()
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	target := filepath.Join(homeDir, fmt.Sprintf("studioforge-sandbox-probe-%d", time.Now().UnixNano()))
	defer os.Remove(target)

	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())

	script := fmt.Sprintf(`echo hi > %q`, target)
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "sandbox-write-outside",
		Kind:        "test",
		Executable:  "/bin/sh",
		Args:        []string{"-c", script},
		Environment: MinimalEnvironment(nil),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for line := range process.Lines() {
		output.WriteString(line.Text)
	}
	result := process.Wait()
	if result.ExitCode == 0 {
		t.Fatalf("exit code = 0, want non-zero; output=%q", output.String())
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("expected %s to not exist", target)
	}
	if !strings.Contains(output.String(), "Operation not permitted") {
		t.Fatalf("output = %q, want it to mention 'Operation not permitted'", output.String())
	}
}

func TestSandboxReapModeIsNotWrapped(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "sandbox-reap-unwrapped",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1"),
		Confine:     ConfinementPolicy{Mode: ConfineReap},
	})
	if err != nil {
		t.Fatal(err)
	}
	if process.confinement != nil {
		t.Fatalf("process.confinement = %+v, want nil for ConfineReap", process.confinement)
	}
	if result := process.Wait(); result.ExitCode != 7 {
		t.Fatalf("result = %+v, want ExitCode 7", result)
	}
}

func TestSandboxMissingWritableRootFailsClosed(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	_, err := supervisor.Start(context.Background(), Spec{
		ID:          "sandbox-missing-root",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1"),
		Confine:     ConfinementPolicy{Mode: ConfineAgent},
	})
	if err == nil {
		t.Fatal("expected Start to fail when WritableRoots is empty")
	}
}
