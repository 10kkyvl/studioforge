package assets

import "fmt"

var transitions = map[string]map[string]bool{"unreviewed": {"quarantined": true, "rejected": true}, "quarantined": {"needs_cleanup": true, "approved": true, "rejected": true}, "needs_cleanup": {"quarantined": true, "approved": true, "rejected": true}}

func ValidateTransition(from, to string) error {
	if transitions[from][to] {
		return nil
	}
	return fmt.Errorf("invalid asset review transition %s -> %s", from, to)
}

type Review struct {
	AssetID           string   `json:"assetId"`
	Status            string   `json:"status"`
	SuspiciousScripts []string `json:"suspiciousScripts"`
	Complexity        string   `json:"complexity"`
	StyleConsistent   bool     `json:"styleConsistent"`
	Notes             string   `json:"notes"`
}
