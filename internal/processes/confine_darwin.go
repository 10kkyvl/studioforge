//go:build darwin

package processes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const sandboxExecPath = "/usr/bin/sandbox-exec"

// sandboxConfinement wraps a command with sandbox-exec. Attach is a no-op:
// sandbox-exec calls sandbox_init then execvp, replacing itself in place, so
// the PID and exit code that os/exec observes are already the tool's own and
// nothing needs to be done once the process has started.
//
// If sandbox_init itself fails (a malformed profile, an unreadable -D path,
// ...), sandbox-exec exits before execvp ever runs. That failure is
// indistinguishable at the Result level from the tool itself failing, but it
// can be told apart by its symptom: sandbox-exec writes a line prefixed
// "sandbox-exec:" to stderr and exits nonzero without producing any of the
// tool's own output. A later step can use that signature (and a doctor probe
// that runs a trivial confined command up front) to surface a clear
// "confinement failed to start" error instead of blaming the tool.
type sandboxConfinement struct {
	mu     sync.Mutex
	dir    string
	pid    int
	killed bool
	closed bool
}

func applyConfinement(cmd *exec.Cmd, spec Spec) (Confinement, error) {
	if err := EnforcesNetworkPolicy(spec.Confine.Network); err != nil {
		return nil, err
	}
	net := spec.Confine.Network.Normalized()
	switch spec.Confine.Mode {
	case ConfineNone:
		return nil, nil
	case ConfineReap:
		return applyReapNetworkConfinement(cmd, net, spec.Environment)
	case ConfineAgent:
	default:
		return nil, fmt.Errorf("processes: unknown confinement mode %q: %w", spec.Confine.Mode, ErrConfinementUnsupported)
	}

	roots := spec.Confine.WritableRoots
	if len(roots) == 0 || roots[0] == "" {
		return nil, errors.New("processes: confine agent mode requires at least one writable root")
	}

	if _, err := os.Stat(sandboxExecPath); err != nil {
		return nil, fmt.Errorf("processes: %s not found: %w", sandboxExecPath, ErrConfinementUnsupported)
	}

	root, err := resolvePath(roots[0])
	if err != nil {
		return nil, fmt.Errorf("resolve writable root %q: %w", roots[0], err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	home, err = resolvePath(home)
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	tmp, err := resolvePath(os.TempDir())
	if err != nil {
		return nil, fmt.Errorf("resolve temp directory: %w", err)
	}

	proxyAddr := ""
	if net == NetworkRegistryOnly {
		proxyAddr = proxyAddressFromEnvironment(spec.Environment)
	}
	profile, err := sandboxProfile(net, proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("build sandbox profile: %w", err)
	}

	defines := [][2]string{{"ROOT", root}, {"HOME", home}, {"TMP", tmp}}
	if net == NetworkRegistryOnly {
		defines = append(defines, [2]string{"PROXY", proxyAddr})
	}
	return newSandboxConfinement(cmd, profile, defines)
}

func applyReapNetworkConfinement(cmd *exec.Cmd, net NetworkPolicy, env []string) (Confinement, error) {
	if net == NetworkUnrestricted {
		return nil, nil
	}
	if _, err := os.Stat(sandboxExecPath); err != nil {
		return nil, fmt.Errorf("processes: %s not found: %w", sandboxExecPath, ErrConfinementUnsupported)
	}
	proxyAddr := ""
	if net == NetworkRegistryOnly {
		proxyAddr = proxyAddressFromEnvironment(env)
	}
	profile, err := sandboxNetworkOnlyProfile(net, proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("build sandbox network profile: %w", err)
	}
	var defines [][2]string
	if net == NetworkRegistryOnly {
		defines = [][2]string{{"PROXY", proxyAddr}}
	}
	return newSandboxConfinement(cmd, profile, defines)
}

func newSandboxConfinement(cmd *exec.Cmd, profile string, defines [][2]string) (Confinement, error) {
	dir, err := os.MkdirTemp("", "studioforge-confine-")
	if err != nil {
		return nil, fmt.Errorf("create confinement profile directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("chmod confinement profile directory: %w", err)
	}
	profilePath := filepath.Join(dir, "profile.sb")
	if err := os.WriteFile(profilePath, []byte(profile), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("write confinement profile: %w", err)
	}

	originalPath := cmd.Path
	originalArgs := cmd.Args
	args := []string{sandboxExecPath}
	for _, define := range defines {
		args = append(args, "-D", define[0]+"="+define[1])
	}
	args = append(args, "-f", profilePath, originalPath)
	cmd.Path = sandboxExecPath
	cmd.Args = append(args, originalArgs[1:]...)

	return &sandboxConfinement{dir: dir}, nil
}

// probeConfinement checks not just that sandbox-exec exists but that it can
// actually compile and run a trivial profile. A file-existence check alone is
// not enough: Apple has deprecated sandbox-exec since 10.10, and the point of
// this probe is to catch the day it stops working, not just the day the
// binary is removed.
func probeConfinement() error {
	if _, err := os.Stat(sandboxExecPath); err != nil {
		return fmt.Errorf("processes: %s not found: %w", sandboxExecPath, ErrConfinementUnsupported)
	}

	dir, err := os.MkdirTemp("", "studioforge-confine-probe-")
	if err != nil {
		return fmt.Errorf("processes: create probe profile directory: %w", err)
	}
	defer os.RemoveAll(dir)

	profilePath := filepath.Join(dir, "probe.sb")
	if err := os.WriteFile(profilePath, []byte("(version 1)\n(allow default)\n"), 0o600); err != nil {
		return fmt.Errorf("processes: write probe profile: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sandboxExecPath, "-f", profilePath, "/usr/bin/true")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("processes: %s failed to run a trivial profile: %w: %w", sandboxExecPath, err, ErrConfinementUnsupported)
	}
	return nil
}

func init() {
	darwinNetworkPolicyProbe = probeNetworkPolicy
}

func probeNetworkPolicy(p NetworkPolicy) error {
	if _, err := os.Stat(sandboxExecPath); err != nil {
		return fmt.Errorf("processes: %s not found: %w", sandboxExecPath, ErrConfinementUnsupported)
	}

	dir, err := os.MkdirTemp("", "studioforge-netpolicy-probe-")
	if err != nil {
		return fmt.Errorf("processes: create network policy probe directory: %w", err)
	}
	defer os.RemoveAll(dir)

	profilePath := filepath.Join(dir, "probe.sb")
	if err := os.WriteFile(profilePath, []byte("(version 1)\n(allow default)\n(deny network*)\n"), 0o600); err != nil {
		return fmt.Errorf("processes: write network policy probe profile: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sandboxExecPath, "-f", profilePath, "/usr/bin/true")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("processes: %s failed to compile a network-denying profile for policy %q: %w: %w", sandboxExecPath, p, err, ErrConfinementUnsupported)
	}
	return nil
}

func proxyAddressFromEnvironment(env []string) string {
	for _, key := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		value := lookupEnvEntry(env, key)
		if value == "" {
			continue
		}
		addr := strings.TrimPrefix(strings.TrimPrefix(value, "http://"), "https://")
		addr = strings.TrimSuffix(addr, "/")
		if addr != "" {
			return addr
		}
	}
	return ""
}

func lookupEnvEntry(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

// resolvePath resolves symlinks (macOS /tmp and /var are symlinks into
// /private) and makes the result absolute, so it matches what the sandbox's
// subpath checks see at run time.
func resolvePath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

func (s *sandboxConfinement) Attach(cmd *exec.Cmd) error {
	s.mu.Lock()
	if cmd.Process != nil {
		s.pid = cmd.Process.Pid
	}
	s.mu.Unlock()
	return nil
}

func (s *sandboxConfinement) Kill() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.killed || s.pid == 0 {
		return nil
	}
	s.killed = true
	return syscall.Kill(-s.pid, syscall.SIGKILL)
}

func (s *sandboxConfinement) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.dir == "" {
		return nil
	}
	return os.RemoveAll(s.dir)
}

// sweepStaleProfiles best-effort removes studioforge-confine-* directories
// left behind in os.TempDir() by a hard crash (kill -9, power loss) that
// skipped a graceful Close. Only directories older than an hour are removed
// so a run that is concurrently starting is never robbed of its own profile.
func sweepStaleProfiles() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-time.Hour)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "studioforge-confine-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(os.TempDir(), entry.Name()))
	}
}
