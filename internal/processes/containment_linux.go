//go:build linux

package processes

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Linux is a CI/future target. bwrap is the supported backend because it can
// create a private mount view without CGO. Required confinement fails closed
// when bwrap is unavailable or its assumptions cannot be met.
func preparePlatformContainment(cmd *exec.Cmd, spec ContainmentSpec) error {
	if !spec.required() {
		return nil
	}
	if err := spec.validate(); err != nil {
		return err
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return fmt.Errorf("bwrap is unavailable; refusing unconfined process: %w", err)
	}
	root, err := filepath.Abs(spec.WorkspaceRoot)
	if err != nil {
		return fmt.Errorf("workspace root: %w", err)
	}
	tmp := spec.TempDir
	if tmp == "" {
		tmp = os.TempDir()
	}
	// Keep the host's read-only system view so dynamically linked tools and
	// package-manager caches continue to work, while writes are limited to the
	// project and temporary directory.
	original := append([]string(nil), cmd.Args...)
	args := []string{"bwrap", "--die-with-parent"}
	if spec.Filesystem == FilesystemFullAccess {
		// Preserve the operator's full filesystem permissions for the highest
		// permission tier. The process is still lifetime/network contained.
		args = append(args, "--bind", "/", "/")
	} else {
		args = append(args, "--ro-bind", "/", "/")
	}
	if spec.Filesystem == FilesystemReadOnly {
		args = append(args, "--ro-bind", root, root)
	} else {
		args = append(args, "--bind", root, root)
	}
	args = append(args, "--bind", tmp, tmp, "--chdir", cmd.Dir)
	switch spec.Network {
	case NetworkUnrestricted:
	case NetworkNone:
		args = append(args, "--unshare-net")
	case NetworkRegistryOnly:
		return fmt.Errorf("registry-only network policy is not supported by the Linux bwrap backend")
	default:
		return fmt.Errorf("unknown network policy: %s", spec.Network)
	}
	args = append(args, "--")
	args = append(args, original...)
	cmd.Path = bwrap
	cmd.Args = args
	return nil
}

func attachPlatformContainment(_ *exec.Cmd, _ ContainmentSpec) (func(), error) {
	return func() {}, nil
}

func cleanupPreparedPlatformContainment(_ *exec.Cmd)                      {}
func preparedNetworkObservations(_ *exec.Cmd) func() []NetworkObservation { return nil }
