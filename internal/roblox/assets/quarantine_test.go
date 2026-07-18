package assets

import "testing"

func TestQuarantineStateMachine(t *testing.T) {
	for _, pair := range [][2]string{{"unreviewed", "quarantined"}, {"quarantined", "needs_cleanup"}, {"quarantined", "approved"}, {"needs_cleanup", "approved"}} {
		if err := ValidateTransition(pair[0], pair[1]); err != nil {
			t.Error(err)
		}
	}
	if err := ValidateTransition("unreviewed", "approved"); err == nil {
		t.Fatal("asset bypassed quarantine")
	}
}
