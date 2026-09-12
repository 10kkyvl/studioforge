package processes

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRequiredContainmentRejectsMissingWorkspace(t *testing.T) {
	s := NewSupervisor()
	defer s.Close(context.Background())
	_, err := s.Start(context.Background(), Spec{
		ID: "missing-root", Kind: "test", Executable: os.Args[0],
		Containment: ContainmentSpec{Mode: ContainmentRequired, Network: NetworkUnrestricted},
	})
	if err == nil || !strings.Contains(err.Error(), "workspace root") {
		t.Fatalf("Start error = %v, want missing workspace root", err)
	}
}

func TestRequiredContainmentRefusesUnsupportedLinuxBackend(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux backend test")
	}
	if _, err := os.Stat("/usr/bin/bwrap"); err == nil {
		t.Skip("bwrap is installed; use the integration test with a user namespace")
	}
	s := NewSupervisor()
	defer s.Close(context.Background())
	_, err := s.Start(context.Background(), Spec{
		ID: "linux-fail-closed", Kind: "test", Executable: os.Args[0],
		WorkingDirectory: t.TempDir(),
		Containment:      ContainmentSpec{Mode: ContainmentRequired, WorkspaceRoot: t.TempDir(), Network: NetworkUnrestricted},
	})
	if err == nil || !strings.Contains(err.Error(), "refusing unconfined") {
		t.Fatalf("Start error = %v, want fail-closed bwrap refusal", err)
	}
}

func TestRequiredContainmentRefusesWriteOutsideWorkspaceOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox integration test")
	}
	root := t.TempDir()
	marker := filepath.Join("/private/tmp", fmt.Sprintf("studioforge-containment-%s", filepath.Base(root)))
	_ = os.Remove(marker)
	s := NewSupervisor()
	defer s.Close(context.Background())
	touch, err := exec.LookPath("touch")
	if err != nil {
		t.Skip("touch is unavailable")
	}
	p, err := s.Start(context.Background(), Spec{
		ID: "darwin-write-boundary", Kind: "test", Executable: touch, Args: []string{marker},
		WorkingDirectory: root,
		Containment:      ContainmentSpec{Mode: ContainmentRequired, WorkspaceRoot: root, Network: NetworkUnrestricted},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Wait()
	if _, err := os.Stat(marker); err == nil {
		_ = os.Remove(marker)
		t.Fatal("contained process wrote outside workspace")
	}
}

func TestDarwinRegistryProfileHasNoWildcardNetworkGrant(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS profile test")
	}
	profile, err := darwinSandboxProfile("/project", "/tmp", FilesystemProjectOnly, NetworkRegistryOnly, []string{"registry.npmjs.org"}, 43123)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(profile, `remote tcp "localhost:43123"`) || strings.Contains(profile, `remote tcp "*:443"`) {
		t.Fatalf("registry-only profile must expose only the loopback proxy: %s", profile)
	}
}

func TestRequiredContainmentRefusesOutboundNetworkOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox integration test")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	root := t.TempDir()
	marker := filepath.Join(root, "connected")
	s := NewSupervisor()
	defer s.Close(context.Background())
	p, err := s.Start(context.Background(), Spec{
		ID: "darwin-network-none", Kind: "test", Executable: os.Args[0],
		WorkingDirectory: root,
		Environment: append(MinimalEnvironment(nil),
			"STUDIOFORGE_HELPER=1",
			"STUDIOFORGE_HELPER_CONNECT_ADDR="+listener.Addr().String(),
			"STUDIOFORGE_HELPER_CONNECT_MARKER="+marker),
		Containment: ContainmentSpec{Mode: ContainmentRequired, WorkspaceRoot: root, Network: NetworkNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Wait()
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("contained process made an outbound connection under network-none")
	}
}

func TestRequiredContainmentRunsGoTestOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS containment acceptance test")
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is unavailable")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	s := NewSupervisor()
	defer s.Close(context.Background())
	p, err := s.Start(context.Background(), Spec{
		ID: "darwin-go-test", Kind: "acceptance", Executable: goPath,
		WorkingDirectory: repoRoot, Args: []string{"test", "./internal/processes", "-run", "TestMinimalEnvironmentPreservesKeychainIdentityWithoutCredentials"},
		Environment: MinimalEnvironment(nil),
		Containment: ContainmentSpec{Mode: ContainmentRequired, WorkspaceRoot: repoRoot, Network: NetworkUnrestricted},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := p.Wait()
	if result.ExitCode != 0 {
		for line := range p.Lines() {
			t.Log(line.Text)
		}
		t.Fatalf("go test inside containment exited %d: %v", result.ExitCode, result.Err)
	}
}

func TestRequiredContainmentRunsRojoBuildOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS containment acceptance test")
	}
	rojo, err := exec.LookPath("rojo")
	if err != nil {
		t.Skip("rojo is unavailable")
	}
	fixture := []byte(`{"name":"Containment test","tree":{"$className":"DataModel","Workspace":{"$className":"Workspace","Baseplate":{"$className":"Part","$properties":{"Anchored":true,"Size":[64,1,64]}}}}}`)
	root := t.TempDir()
	projectFile := filepath.Join(root, "default.project.json")
	if err := os.WriteFile(projectFile, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewSupervisor()
	defer s.Close(context.Background())
	p, err := s.Start(context.Background(), Spec{
		ID: "darwin-rojo-build", Kind: "acceptance", Executable: rojo,
		WorkingDirectory: root, Args: []string{"build", projectFile, "-o", filepath.Join(root, "build.rbxl")},
		Environment: MinimalEnvironment(nil),
		Containment: ContainmentSpec{Mode: ContainmentRequired, WorkspaceRoot: root, Network: NetworkUnrestricted},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := p.Wait()
	if result.ExitCode != 0 {
		for line := range p.Lines() {
			t.Log(line.Text)
		}
		t.Fatalf("rojo build inside containment exited %d: %v", result.ExitCode, result.Err)
	}
	if _, err := os.Stat(filepath.Join(root, "build.rbxl")); err != nil {
		t.Fatalf("rojo did not create the output place: %v", err)
	}
}

func TestRequiredContainmentRunsNPMInstallOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("STUDIOFORGE_CONTAINMENT_NETWORK_SMOKE") != "1" {
		t.Skip("set STUDIOFORGE_CONTAINMENT_NETWORK_SMOKE=1 on macOS for the live npm registry check")
	}
	npm, err := exec.LookPath("npm")
	if err != nil {
		t.Skip("npm is unavailable")
	}
	root := t.TempDir()
	packageJSON := []byte(`{"name":"studioforge-containment-smoke","private":true,"dependencies":{"is-number":"7.0.0"}}`)
	if err := os.WriteFile(filepath.Join(root, "package.json"), packageJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewSupervisor()
	defer s.Close(context.Background())
	p, err := s.Start(context.Background(), Spec{
		ID: "darwin-npm-install", Kind: "acceptance", Executable: npm,
		WorkingDirectory: root, Args: []string{"install", "--ignore-scripts", "--no-audit", "--no-fund", "--package-lock=false"},
		Environment: MinimalEnvironment(nil),
		Containment: ContainmentSpec{Mode: ContainmentRequired, WorkspaceRoot: root, Network: NetworkRegistryOnly, RegistryHosts: []string{"registry.npmjs.org"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := p.Wait()
	if result.ExitCode != 0 {
		for line := range p.Lines() {
			t.Log(line.Text)
		}
		t.Fatalf("npm install inside containment exited %d: %v", result.ExitCode, result.Err)
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules", "is-number", "package.json")); err != nil {
		t.Fatalf("npm did not install the dependency: %v", err)
	}
}

func TestRequiredContainmentAllowsConfiguredRegistryOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox integration test")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is unavailable")
	}
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl is unavailable")
	}
	root := t.TempDir()
	s := NewSupervisor()
	defer s.Close(context.Background())
	p, err := s.Start(context.Background(), Spec{
		ID: "darwin-network-registry-allow", Kind: "acceptance", Executable: curl,
		WorkingDirectory: root, Args: []string{"-fsS", "--max-time", "10", "https://registry.npmjs.org/-/ping"},
		Environment: MinimalEnvironment(nil),
		Containment: ContainmentSpec{Mode: ContainmentRequired, WorkspaceRoot: root, Network: NetworkRegistryOnly, RegistryHosts: []string{"registry.npmjs.org"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := p.Wait()
	if result.ExitCode != 0 {
		for line := range p.Lines() {
			t.Log(line.Text)
		}
		t.Fatalf("configured registry request was denied: %v", result.Err)
	}
	observations := p.NetworkObservations()
	if len(observations) == 0 || !observations[0].Allowed || observations[0].Host != "registry.npmjs.org" {
		t.Fatalf("registry proxy observations = %#v, want an allowed registry request", observations)
	}
}

func TestRequiredContainmentDeniesNonRegistryThroughProxyOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox integration test")
	}
	root := t.TempDir()
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl is unavailable")
	}
	s := NewSupervisor()
	defer s.Close(context.Background())
	p, err := s.Start(context.Background(), Spec{
		ID: "darwin-network-registry-deny", Kind: "acceptance", Executable: curl,
		WorkingDirectory: root, Args: []string{"-fsS", "--max-time", "10", "https://example.com/"},
		Environment: MinimalEnvironment(nil),
		Containment: ContainmentSpec{Mode: ContainmentRequired, WorkspaceRoot: root, Network: NetworkRegistryOnly, RegistryHosts: []string{"registry.npmjs.org"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := p.Wait()
	if result.ExitCode == 0 {
		t.Fatal("non-registry request unexpectedly succeeded through registry proxy")
	}
	observations := p.NetworkObservations()
	if len(observations) == 0 || observations[0].Allowed || observations[0].Host != "example.com" {
		t.Fatalf("registry proxy observations = %#v, want a denied non-registry request", observations)
	}
}

func TestRequiredContainmentDeniesDirectOutboundBypassOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox integration test")
	}
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl is unavailable")
	}
	root := t.TempDir()
	s := NewSupervisor()
	defer s.Close(context.Background())
	p, err := s.Start(context.Background(), Spec{
		ID: "darwin-network-registry-direct-bypass", Kind: "acceptance", Executable: curl,
		WorkingDirectory: root, Args: []string{"-fsS", "--noproxy", "*", "--connect-timeout", "3", "https://example.com/"},
		Environment: MinimalEnvironment(nil),
		Containment: ContainmentSpec{Mode: ContainmentRequired, WorkspaceRoot: root, Network: NetworkRegistryOnly, RegistryHosts: []string{"registry.npmjs.org"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := p.Wait()
	if result.ExitCode == 0 {
		t.Fatal("direct outbound request bypassed the registry proxy")
	}
}

func BenchmarkRequiredContainmentStart(b *testing.B) {
	if runtime.GOOS != "darwin" {
		b.Skip("benchmark is measured on the shipped macOS backend")
	}
	touch, err := exec.LookPath("touch")
	if err != nil {
		b.Skip("touch is unavailable")
	}
	root := b.TempDir()
	s := NewSupervisor()
	defer s.Close(context.Background())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := s.Start(context.Background(), Spec{
			ID: fmt.Sprintf("bench-%d", i), Kind: "benchmark", Executable: touch,
			WorkingDirectory: root, Args: []string{filepath.Join(root, fmt.Sprintf("%d", i))},
			Containment: ContainmentSpec{Mode: ContainmentRequired, WorkspaceRoot: root, Network: NetworkUnrestricted},
		})
		if err != nil {
			b.Fatal(err)
		}
		_ = p.Wait()
	}
}
