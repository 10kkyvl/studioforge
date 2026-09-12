package processes

import (
	"errors"
)

// ContainmentMode controls whether an agent child must be placed in an OS
// boundary. Disabled is used only by trusted daemon helpers; Required is the
// mode used by agent-started commands and is fail-closed.
type ContainmentMode string

const (
	ContainmentDisabled ContainmentMode = "disabled"
	ContainmentRequired ContainmentMode = "required"
)

// NetworkPolicy is intentionally independent from filesystem/process
// containment. The default preserves package-manager compatibility; the other
// values are enforced by the platform backend when supported.
type NetworkPolicy string

const (
	NetworkUnrestricted NetworkPolicy = "unrestricted"
	NetworkRegistryOnly NetworkPolicy = "registry-only"
	NetworkNone         NetworkPolicy = "none"
)

// NetworkObservation records only the destination authority and decision made
// by a registry proxy. It intentionally excludes paths, query strings, and
// request/response bodies.
type NetworkObservation struct {
	Host    string
	Port    string
	Allowed bool
}

type FilesystemPolicy string

const (
	FilesystemProjectOnly FilesystemPolicy = "project-only"
	FilesystemReadOnly    FilesystemPolicy = "read-only"
	FilesystemFullAccess  FilesystemPolicy = "full-access"
)

// ContainmentSpec is part of a process start request rather than global
// process state so a future per-agent setting cannot accidentally affect other
// runs.
type ContainmentSpec struct {
	Mode          ContainmentMode
	Filesystem    FilesystemPolicy
	WorkspaceRoot string
	TempDir       string
	Network       NetworkPolicy
	// RegistryHosts is used for registry-only mode. Hosts are names, not URLs;
	// the platform backend adds the appropriate package-manager ports.
	RegistryHosts []string
}

// DefaultAgentContainment enables the shipped backends. Linux remains an
// explicit opt-in until bwrap is part of a supported distribution image; a
// caller that requests Required still gets fail-closed behavior there.
func DefaultAgentContainment(root string) ContainmentSpec {
	return ContainmentSpec{Mode: ContainmentRequired, Filesystem: FilesystemProjectOnly, WorkspaceRoot: root, Network: NetworkUnrestricted}
}

func (c ContainmentSpec) required() bool { return c.Mode == ContainmentRequired }

func (c ContainmentSpec) validate() error {
	if !c.required() {
		return nil
	}
	if c.WorkspaceRoot == "" {
		return errors.New("workspace root is required")
	}
	if c.Network == "" {
		return errors.New("network policy is required")
	}
	if c.Filesystem == "" {
		c.Filesystem = FilesystemProjectOnly
	}
	if c.Filesystem != FilesystemProjectOnly && c.Filesystem != FilesystemReadOnly && c.Filesystem != FilesystemFullAccess {
		return errors.New("unknown filesystem policy: " + string(c.Filesystem))
	}
	switch c.Network {
	case NetworkUnrestricted, NetworkNone:
	case NetworkRegistryOnly:
		if len(c.RegistryHosts) == 0 {
			return errors.New("registry-only policy requires at least one registry host")
		}
	default:
		return errors.New("unknown network policy: " + string(c.Network))
	}
	return nil
}
