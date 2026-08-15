package processes

import (
	"errors"
	"runtime"
	"testing"
)

func TestNetworkPolicyNormalisesEmptyToUnrestricted(t *testing.T) {
	if got := NetworkPolicy("").Normalized(); got != NetworkUnrestricted {
		t.Fatalf("Normalized() = %q, want %q", got, NetworkUnrestricted)
	}
	if !NetworkPolicy("").Valid() {
		t.Fatal("an empty network policy should be valid, since it normalizes to unrestricted")
	}
	if got := NetworkPolicy("  ").Normalized(); got != NetworkUnrestricted {
		t.Fatalf("Normalized() = %q, want %q", got, NetworkUnrestricted)
	}
	if got := NetworkPolicy("REGISTRY-ONLY").Normalized(); got != NetworkRegistryOnly {
		t.Fatalf("Normalized() = %q, want %q", got, NetworkRegistryOnly)
	}
}

func TestNetworkPolicyRejectsUnknownValues(t *testing.T) {
	for _, bad := range []NetworkPolicy{"banana", "unrestricted-ish", "off", "sandboxed"} {
		if bad.Valid() {
			t.Fatalf("%q should not be a valid network policy", bad)
		}
	}
	for _, good := range []NetworkPolicy{NetworkUnrestricted, NetworkRegistryOnly, NetworkNone, "REGISTRY-ONLY", ""} {
		if !good.Valid() {
			t.Fatalf("%q should be a valid network policy", good)
		}
	}
}

func TestEnforcesNetworkPolicyMatchesThePlatform(t *testing.T) {
	if err := EnforcesNetworkPolicy(NetworkUnrestricted); err != nil {
		t.Fatalf("unrestricted must never be refused, on any platform: %v", err)
	}
	err := EnforcesNetworkPolicy(NetworkNone)
	if runtime.GOOS == "darwin" {
		if err != nil {
			t.Fatalf("darwin must accept a strict network policy at the type level (enforcement lands separately): %v", err)
		}
		return
	}
	if err == nil {
		t.Fatal("expected a strict network policy to be refused on a platform with no enforcement")
	}
	if !errors.Is(err, ErrNetworkPolicyUnsupported) {
		t.Fatalf("err = %v, want it to wrap ErrNetworkPolicyUnsupported", err)
	}
}
