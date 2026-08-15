package processes

import (
	"errors"
	"os"
	"os/exec"
)

type ConfinementMode string

const (
	ConfineNone  ConfinementMode = ""
	ConfineReap  ConfinementMode = "reap"
	ConfineAgent ConfinementMode = "agent"
)

const defaultMaxProcesses uint32 = 128
const defaultMaxMemoryBytes uint64 = 8 << 30

type ConfinementPolicy struct {
	Mode           ConfinementMode
	WritableRoots  []string
	MaxProcesses   uint32
	MaxMemoryBytes uint64
	Network        NetworkPolicy
}

func (p ConfinementPolicy) resolved() ConfinementPolicy {
	if p.MaxProcesses == 0 {
		p.MaxProcesses = defaultMaxProcesses
	}
	if p.MaxMemoryBytes == 0 {
		p.MaxMemoryBytes = defaultMaxMemoryBytes
	}
	p.Network = p.Network.Normalized()
	return p
}

type Confinement interface {
	Attach(cmd *exec.Cmd) error
	Kill() error
	Close() error
}

var ErrConfinementUnsupported = errors.New("processes: OS confinement is not available on this platform")

func allowUnconfined() bool {
	return os.Getenv("STUDIOFORGE_ALLOW_UNCONFINED") == "1"
}

// ProbeConfinement reports whether ConfineAgent confinement can actually be
// established on this platform right now. It exists so an operator can learn
// that OS confinement is unavailable before a run starts (see
// internal/diagnostics's doctor check), instead of finding out only when
// run_command fails mid-run. It returns nil when confinement is available,
// or a descriptive error otherwise.
func ProbeConfinement() error {
	return probeConfinement()
}

func ProbeNetworkPolicy(p NetworkPolicy) error {
	return EnforcesNetworkPolicy(p)
}
