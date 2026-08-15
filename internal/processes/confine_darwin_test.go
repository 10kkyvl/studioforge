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

func TestSandboxNonePolicyBlocksOutboundConnections(t *testing.T) {
	if _, err := os.Stat("/usr/bin/curl"); err != nil {
		t.Skip("curl not present on this runner")
	}
	root := t.TempDir()
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())

	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "sandbox-network-none",
		Kind:        "test",
		Executable:  "/usr/bin/curl",
		Args:        []string{"-sS", "--max-time", "5", "https://example.com"},
		Environment: MinimalEnvironment(nil),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{root}, Network: NetworkNone},
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
		t.Fatalf("exit code = 0, want non-zero for a network policy %q outbound curl; output=%q", NetworkNone, output.String())
	}
}

func TestSandboxDangerProfileStillHonoursNetworkNone(t *testing.T) {
	if _, err := os.Stat("/usr/bin/curl"); err != nil {
		t.Skip("curl not present on this runner")
	}
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())

	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "sandbox-danger-network-none",
		Kind:        "test",
		Executable:  "/usr/bin/curl",
		Args:        []string{"-sS", "--max-time", "5", "https://example.com"},
		Environment: MinimalEnvironment(nil),
		Confine:     ConfinementPolicy{Mode: ConfineReap, Network: NetworkNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if process.confinement == nil {
		t.Fatal("expected ConfineReap to still be wrapped in sandbox-exec when a strict network policy is set")
	}
	var output strings.Builder
	for line := range process.Lines() {
		output.WriteString(line.Text)
	}
	result := process.Wait()
	if result.ExitCode == 0 {
		t.Fatalf("exit code = 0, want non-zero for a danger-full-access process under network policy %q; output=%q", NetworkNone, output.String())
	}
}

func TestSandboxUnrestrictedPolicyLeavesNetworkAlone(t *testing.T) {
	if _, err := os.Stat("/usr/bin/curl"); err != nil {
		t.Skip("curl not present on this runner")
	}
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())

	baseline, err := supervisor.Start(context.Background(), Spec{
		ID:          "sandbox-network-baseline",
		Kind:        "test",
		Executable:  "/usr/bin/curl",
		Args:        []string{"-sS", "--max-time", "5", "https://example.com"},
		Environment: MinimalEnvironment(nil),
		Confine:     ConfinementPolicy{Mode: ConfineNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	baselineResult := baseline.Wait()
	if baselineResult.ExitCode != 0 {
		t.Skip("no outbound network access on this runner; cannot assert unrestricted is no worse than the baseline")
	}

	root := t.TempDir()
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "sandbox-network-unrestricted",
		Kind:        "test",
		Executable:  "/usr/bin/curl",
		Args:        []string{"-sS", "--max-time", "5", "https://example.com"},
		Environment: MinimalEnvironment(nil),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{root}, Network: NetworkUnrestricted},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for line := range process.Lines() {
		output.WriteString(line.Text)
	}
	result := process.Wait()
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0: an unrestricted network policy must be no worse than the unconfined baseline; output=%q", result.ExitCode, output.String())
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
