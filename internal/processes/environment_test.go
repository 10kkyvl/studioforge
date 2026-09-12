package processes

import (
	"strings"
	"testing"
)

func TestMinimalEnvironmentPreservesKeychainIdentityWithoutCredentials(t *testing.T) {
	t.Setenv("USER", "studioforge-test")
	t.Setenv("ANTHROPIC_API_KEY", "private-not-inherited")
	t.Setenv("STUDIOFORGE_PRIVATE_TOKEN", "private-not-inherited")
	env := MinimalEnvironment(nil)
	found := false
	for _, entry := range env {
		if entry == "USER=studioforge-test" {
			found = true
		}
		if strings.Contains(entry, "private-not-inherited") {
			t.Fatal("credential propagated")
		}
	}
	if !found {
		t.Fatal("Keychain account identity lost")
	}
}
