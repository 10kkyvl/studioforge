package processes

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

type NetworkPolicy string

const (
	NetworkUnrestricted NetworkPolicy = "unrestricted"
	NetworkRegistryOnly NetworkPolicy = "registry-only"
	NetworkNone         NetworkPolicy = "none"
)

func (p NetworkPolicy) Normalized() NetworkPolicy {
	trimmed := NetworkPolicy(strings.ToLower(strings.TrimSpace(string(p))))
	if trimmed == "" {
		return NetworkUnrestricted
	}
	return trimmed
}

func (p NetworkPolicy) Valid() bool {
	switch p.Normalized() {
	case NetworkUnrestricted, NetworkRegistryOnly, NetworkNone:
		return true
	default:
		return false
	}
}

var ErrNetworkPolicyUnsupported = errors.New("processes: this network policy cannot be enforced on this platform")

var darwinNetworkPolicyProbe func(NetworkPolicy) error

func EnforcesNetworkPolicy(p NetworkPolicy) error {
	policy := p.Normalized()
	if policy == NetworkUnrestricted {
		return nil
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("%w: network policy %q requires macOS, this host is %s", ErrNetworkPolicyUnsupported, policy, runtime.GOOS)
	}
	if darwinNetworkPolicyProbe == nil {
		return fmt.Errorf("%w: network policy enforcement is not wired up in this build", ErrNetworkPolicyUnsupported)
	}
	if err := darwinNetworkPolicyProbe(policy); err != nil {
		return fmt.Errorf("%w: %v", ErrNetworkPolicyUnsupported, err)
	}
	return nil
}
