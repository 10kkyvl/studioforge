package processes

import (
	"context"
	"errors"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var driveLetterPath = regexp.MustCompile(`[A-Za-z]:[\\/]`)

func TestSandboxProfileOrderingPerNetworkPolicy(t *testing.T) {
	const proxyAddr = "127.0.0.1:38080"
	cases := []struct {
		name string
		net  NetworkPolicy
		addr string
	}{
		{"unrestricted", NetworkUnrestricted, ""},
		{"none", NetworkNone, ""},
		{"registry-only", NetworkRegistryOnly, proxyAddr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile, err := sandboxProfile(tc.net, tc.addr)
			if err != nil {
				t.Fatalf("sandboxProfile(%q, %q) unexpected error: %v", tc.net, tc.addr, err)
			}
			idxAllowDefault := strings.Index(profile, `(allow default)`)
			idxDenyWrite := strings.Index(profile, `(deny file-write*)`)
			idxAllowWriteRoot := strings.Index(profile, `(allow file-write*`)
			if idxAllowDefault < 0 || idxDenyWrite < 0 || idxAllowWriteRoot < 0 {
				t.Fatalf("profile missing expected clauses: %s", profile)
			}
			if !(idxAllowDefault < idxDenyWrite && idxDenyWrite < idxAllowWriteRoot) {
				t.Fatalf("expected ordering allow-default < deny-write* < allow-write-root, got %d, %d, %d", idxAllowDefault, idxDenyWrite, idxAllowWriteRoot)
			}
			if !strings.Contains(profile[idxAllowWriteRoot:], `(param "ROOT")`) {
				t.Fatalf("expected the writable-root allow clause to reference (param \"ROOT\"): %s", profile)
			}
			if strings.Contains(profile, "/Users/") {
				t.Fatalf("profile must not contain an interpolated path, got: %s", profile)
			}
			if driveLetterPath.MatchString(profile) {
				t.Fatalf("profile must not contain an interpolated drive-letter path, got: %s", profile)
			}
			if proxyAddr != "" && strings.Contains(profile, proxyAddr) {
				t.Fatalf("profile must not contain an interpolated proxy address, got: %s", profile)
			}

			switch tc.net {
			case NetworkUnrestricted:
				if strings.Contains(profile, "network") {
					t.Fatalf("unrestricted profile must not mention network at all, got: %s", profile)
				}
			case NetworkNone:
				idxDenyNetwork := strings.Index(profile, `(deny network*)`)
				if idxDenyNetwork < 0 {
					t.Fatalf("none profile is missing (deny network*): %s", profile)
				}
				if idxDenyNetwork < idxAllowDefault {
					t.Fatalf("expected (deny network*) after (allow default), got deny=%d allow=%d", idxDenyNetwork, idxAllowDefault)
				}
			case NetworkRegistryOnly:
				idxDenyNetwork := strings.Index(profile, `(deny network*)`)
				idxAllowNetwork := strings.Index(profile, `(allow network-outbound`)
				if idxDenyNetwork < 0 || idxAllowNetwork < 0 {
					t.Fatalf("registry-only profile is missing its network clauses: %s", profile)
				}
				if !(idxAllowDefault < idxDenyNetwork && idxDenyNetwork < idxAllowNetwork) {
					t.Fatalf("expected ordering allow-default < deny-network* < allow-network-outbound, got %d, %d, %d", idxAllowDefault, idxDenyNetwork, idxAllowNetwork)
				}
				if !strings.Contains(profile[idxAllowNetwork:], `(param "PROXY")`) {
					t.Fatalf("expected the network allow clause to reference (param \"PROXY\"): %s", profile)
				}
			}
		})
	}
}

func TestSandboxProfileRegistryOnlyFailsClosedWithoutAProxyAddress(t *testing.T) {
	if _, err := sandboxProfile(NetworkRegistryOnly, ""); err == nil {
		t.Fatal("expected sandboxProfile to fail closed when registry-only has no proxy address configured")
	}
	if _, err := sandboxNetworkOnlyProfile(NetworkRegistryOnly, ""); err == nil {
		t.Fatal("expected sandboxNetworkOnlyProfile to fail closed when registry-only has no proxy address configured")
	}
}

func TestConfineNoneStartsNormally(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "confine-none",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1"),
		Confine:     ConfinementPolicy{Mode: ConfineNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result := process.Wait(); result.ExitCode != 7 {
		t.Fatalf("result = %+v, want ExitCode 7", result)
	}
}

// TestConfineAgentUnsupportedFailsClosed checks that Start fails closed (does
// not silently run unconfined) when confinement is requested but not
// available. Windows and darwin get real implementations in later steps, so
// this is scoped to the platforms whose applyConfinement stays a stub, and
// stays correct once those two land.
func TestConfineAgentUnsupportedFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("confinement is implemented natively on this platform")
	}
	// CI sets this so the rest of the suite can run on Linux, where there is no
	// implementation. The whole point of this test is the refusal, so it has to
	// look at the platform without the escape hatch.
	t.Setenv("STUDIOFORGE_ALLOW_UNCONFINED", "")
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	_, err := supervisor.Start(context.Background(), Spec{
		ID:          "confine-agent-unsupported",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1"),
		Confine:     ConfinementPolicy{Mode: ConfineAgent},
	})
	if err == nil {
		t.Fatal("expected Start to fail when confinement is unsupported")
	}
	if !errors.Is(err, ErrConfinementUnsupported) {
		t.Fatalf("err = %v, want it to wrap ErrConfinementUnsupported", err)
	}
}

func TestUnrestrictedNetworkPolicyStartsEverywhere(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "unrestricted-network",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1"),
		Confine:     ConfinementPolicy{Mode: ConfineNone, Network: NetworkUnrestricted},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result := process.Wait(); result.ExitCode != 7 {
		t.Fatalf("result = %+v, want ExitCode 7", result)
	}
}

func TestAllowUnconfinedDoesNotBypassNetworkPolicy(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin has a real ConfineAgent implementation, so STUDIOFORGE_ALLOW_UNCONFINED never needs to bypass anything here")
	}
	t.Setenv("STUDIOFORGE_ALLOW_UNCONFINED", "1")
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "network-policy-not-bypassed",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1"),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, Network: NetworkNone, WritableRoots: []string{t.TempDir()}},
	})
	if err == nil {
		t.Fatal("expected Start to fail: STUDIOFORGE_ALLOW_UNCONFINED must not bypass a strict network policy")
	}
	if !errors.Is(err, ErrNetworkPolicyUnsupported) {
		t.Fatalf("err = %v, want it to wrap ErrNetworkPolicyUnsupported", err)
	}
	if process != nil {
		t.Fatalf("expected nil process, got %+v", process)
	}
}
