//go:build !darwin

package processes

import "fmt"

func darwinSandboxProfile(_, _ string, _ FilesystemPolicy, _ NetworkPolicy, _ []string, _ int) (string, error) {
	return "", fmt.Errorf("macOS sandbox profile is unavailable on this platform")
}
