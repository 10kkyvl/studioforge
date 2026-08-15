//go:build !windows && !darwin

package processes

import (
	"fmt"
	"log/slog"
	"os/exec"
)

// sweepStaleProfiles has nothing to sweep on this platform; it exists so
// NewSupervisor's call compiles everywhere.
func sweepStaleProfiles() {}

// applyConfinement has no real implementation on this platform, so a policy
// that claims a boundary fails closed. StudioForge does not ship on Linux;
// contributors and CI that need to run the suite there set
// STUDIOFORGE_ALLOW_UNCONFINED=1, which is named in the error so nobody has to
// go looking for it. ConfineReap claims no boundary, only reaping, which the
// process group already provides here.
func applyConfinement(cmd *exec.Cmd, spec Spec) (Confinement, error) {
	if err := EnforcesNetworkPolicy(spec.Confine.Network); err != nil {
		return nil, err
	}
	switch spec.Confine.Mode {
	case ConfineNone, ConfineReap:
		return nil, nil
	}
	if allowUnconfined() {
		slog.Warn("OS confinement is unavailable on this platform, starting process unconfined", "reason", "STUDIOFORGE_ALLOW_UNCONFINED=1")
		return nil, nil
	}
	return nil, fmt.Errorf("%w (set STUDIOFORGE_ALLOW_UNCONFINED=1 to run without it)", ErrConfinementUnsupported)
}

// probeConfinement reports the same fixed answer applyConfinement would give
// for ConfineAgent: there is no real implementation on this platform. It
// deliberately ignores STUDIOFORGE_ALLOW_UNCONFINED - the escape hatch lets a
// run proceed unconfined, but the doctor check exists to tell the operator
// plainly that no boundary is actually enforced here.
func probeConfinement() error {
	return fmt.Errorf("%w (set STUDIOFORGE_ALLOW_UNCONFINED=1 to run without it)", ErrConfinementUnsupported)
}
