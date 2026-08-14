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

func TestSandboxProfileOrdering(t *testing.T) {
	profile := sandboxProfile()
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
