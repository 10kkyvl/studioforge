//go:build !darwin && !linux && !windows

package processes

import (
	"fmt"
	"os/exec"
)

func preparePlatformContainment(_ *exec.Cmd, spec ContainmentSpec) error {
	if spec.required() {
		return fmt.Errorf("OS process confinement is unsupported on this platform")
	}
	return nil
}

func attachPlatformContainment(_ *exec.Cmd, _ ContainmentSpec) (func(), error) { return nil, nil }

func cleanupPreparedPlatformContainment(_ *exec.Cmd)                      {}
func preparedNetworkObservations(_ *exec.Cmd) func() []NetworkObservation { return nil }
