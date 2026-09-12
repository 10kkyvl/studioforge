package processes

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// prepareContainment lets a platform add process creation attributes before
// exec.Cmd.Start. It must not silently downgrade a required request.
func prepareContainment(cmd *exec.Cmd, spec ContainmentSpec) error {
	if err := prepareContainmentEnvironment(cmd, spec); err != nil {
		return err
	}
	return preparePlatformContainment(cmd, spec)
}

// allocateContainmentTemp gives each contained process its own writable cache
// root. Callers must run the returned cleanup after cmd.Wait.
func allocateContainmentTemp(spec *ContainmentSpec) (func(), error) {
	if !spec.required() || spec.TempDir != "" {
		return func() {}, nil
	}
	tmp, err := os.MkdirTemp(os.TempDir(), "studioforge-agent-")
	if err != nil {
		return nil, err
	}
	spec.TempDir = tmp
	return func() { _ = os.RemoveAll(tmp) }, nil
}

// AllocateContainmentTemp reserves the per-run writable directory for a
// provider that starts its command outside Supervisor.
func AllocateContainmentTemp(spec *ContainmentSpec) (func(), error) {
	return allocateContainmentTemp(spec)
}

func prepareContainmentEnvironment(cmd *exec.Cmd, spec ContainmentSpec) error {
	if !spec.required() {
		return nil
	}
	tmp := spec.TempDir
	if tmp == "" {
		tmp = os.TempDir()
	}
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	cacheRoot := filepath.Join(tmp, "studioforge-agent-cache")
	for _, name := range []string{"go-build", "go-mod", "go", "npm", "cargo", "pip", "xdg"} {
		if err := os.MkdirAll(filepath.Join(cacheRoot, name), 0o700); err != nil {
			return err
		}
	}
	values := map[string]string{
		"TMPDIR":           tmp,
		"TMP":              tmp,
		"TEMP":             tmp,
		"GOCACHE":          filepath.Join(cacheRoot, "go-build"),
		"GOMODCACHE":       filepath.Join(cacheRoot, "go-mod"),
		"GOPATH":           filepath.Join(cacheRoot, "go"),
		"npm_config_cache": filepath.Join(cacheRoot, "npm"),
		"CARGO_HOME":       filepath.Join(cacheRoot, "cargo"),
		"PIP_CACHE_DIR":    filepath.Join(cacheRoot, "pip"),
		"XDG_CACHE_HOME":   filepath.Join(cacheRoot, "xdg"),
	}
	if cmd.Env == nil {
		cmd.Env = MinimalEnvironment(nil)
	}
	managed := make(map[string]bool, len(values))
	for key := range values {
		managed[strings.ToUpper(key)] = true
	}
	filtered := make([]string, 0, len(cmd.Env)+len(values))
	for _, entry := range cmd.Env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && managed[strings.ToUpper(key)] {
			continue
		}
		filtered = append(filtered, entry)
	}
	cmd.Env = filtered
	for key, value := range values {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	return nil
}

// startContainedCommand starts a command and returns cleanup for any platform
// handle that must remain live for the lifetime of the child.
func startContainedCommand(cmd *exec.Cmd, spec ContainmentSpec) (func(), error) {
	if err := cmd.Start(); err != nil {
		cleanupPlatformPreparation(cmd)
		return nil, err
	}
	cleanup, err := attachPlatformContainment(cmd, spec)
	if err != nil {
		// A required boundary must never become a best-effort boundary. Kill the
		// just-started tree before returning the error to the caller.
		_ = forceKillTree(cmd)
		_ = cmd.Wait()
		cleanupPlatformPreparation(cmd)
		return nil, err
	}
	return cleanup, nil
}

func cleanupPlatformPreparation(cmd *exec.Cmd) { cleanupPreparedPlatformContainment(cmd) }

func networkObservationReader(cmd *exec.Cmd) func() []NetworkObservation {
	return preparedNetworkObservations(cmd)
}

// PrepareCommand applies the platform wrapper/creation settings to a command
// that a provider starts itself (for example Claude Code).
func PrepareCommand(cmd *exec.Cmd, spec Spec) error {
	return prepareContainment(cmd, spec.Containment)
}

// StartCommand starts a previously prepared command and attaches the required
// platform boundary before returning. The cleanup callback must run after
// cmd.Wait has completed.
func StartCommand(cmd *exec.Cmd, spec Spec) (func(), error) {
	return startContainedCommand(cmd, spec.Containment)
}
