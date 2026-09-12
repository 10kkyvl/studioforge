//go:build darwin

package processes

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var preparedDarwinProxies = struct {
	sync.Mutex
	byCommand map[*exec.Cmd]*registryProxy
}{byCommand: map[*exec.Cmd]*registryProxy{}}

func preparePlatformContainment(cmd *exec.Cmd, spec ContainmentSpec) error {
	if !spec.required() {
		return nil
	}
	if err := spec.validate(); err != nil {
		return err
	}
	sandbox, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return fmt.Errorf("sandbox-exec is unavailable: %w", err)
	}
	root, err := realDirectory(spec.WorkspaceRoot)
	if err != nil {
		return fmt.Errorf("workspace root: %w", err)
	}
	tmp := spec.TempDir
	if tmp == "" {
		tmp = os.TempDir()
	}
	tmp, err = realDirectory(tmp)
	if err != nil {
		return fmt.Errorf("temp directory: %w", err)
	}
	var proxy *registryProxy
	proxyPort := 0
	if spec.Network == NetworkRegistryOnly {
		proxy, err = newRegistryProxy(spec.RegistryHosts)
		if err != nil {
			return err
		}
		proxyPort = proxy.Port()
		preparedDarwinProxies.Lock()
		preparedDarwinProxies.byCommand[cmd] = proxy
		preparedDarwinProxies.Unlock()
		cmd.Env = withProxyEnvironment(cmd.Env, proxy.URL())
	}
	profile, err := darwinSandboxProfile(root, tmp, spec.Filesystem, spec.Network, spec.RegistryHosts, proxyPort)
	if err != nil {
		cleanupPreparedPlatformContainment(cmd)
		return err
	}
	original := append([]string(nil), cmd.Args...)
	cmd.Path = sandbox
	cmd.Args = append([]string{"sandbox-exec", "-p", profile, "--"}, original...)
	return nil
}

func attachPlatformContainment(cmd *exec.Cmd, spec ContainmentSpec) (func(), error) {
	if spec.required() {
		return func() { cleanupPreparedPlatformContainment(cmd) }, nil
	}
	return nil, nil
}

func realDirectory(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", path)
	}
	return filepath.Clean(resolved), nil
}

func darwinSandboxProfile(root, tmp string, filesystem FilesystemPolicy, policy NetworkPolicy, hosts []string, proxyPort int) (string, error) {
	var b strings.Builder
	b.WriteString("(version 1) (allow default)")
	switch filesystem {
	case FilesystemFullAccess:
	case FilesystemReadOnly:
		b.WriteString(" (deny file-write*)")
		fmt.Fprintf(&b, " (allow file-write* (subpath %s))", sandboxString(tmp))
	default:
		b.WriteString(" (deny file-write*)")
		fmt.Fprintf(&b, " (allow file-write* (subpath %s))", sandboxString(root))
		fmt.Fprintf(&b, " (allow file-write* (subpath %s))", sandboxString(tmp))
	}
	switch policy {
	case NetworkUnrestricted:
	case NetworkNone:
		b.WriteString(" (deny network-outbound)")
	case NetworkRegistryOnly:
		if proxyPort <= 0 || len(hosts) == 0 {
			return "", fmt.Errorf("registry-only network policy requires a loopback proxy")
		}
		b.WriteString(" (deny network-outbound)")
		fmt.Fprintf(&b, " (allow network-outbound (remote tcp \"localhost:%d\"))", proxyPort)
	default:
		return "", fmt.Errorf("unknown network policy: %s", policy)
	}
	return b.String(), nil
}

func cleanupPreparedPlatformContainment(cmd *exec.Cmd) {
	preparedDarwinProxies.Lock()
	proxy := preparedDarwinProxies.byCommand[cmd]
	delete(preparedDarwinProxies.byCommand, cmd)
	preparedDarwinProxies.Unlock()
	if proxy != nil {
		proxy.Close()
	}
}

func preparedNetworkObservations(cmd *exec.Cmd) func() []NetworkObservation {
	preparedDarwinProxies.Lock()
	proxy := preparedDarwinProxies.byCommand[cmd]
	preparedDarwinProxies.Unlock()
	if proxy == nil {
		return nil
	}
	return proxy.Observations
}

func sandboxString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return "\"" + s + "\""
}
