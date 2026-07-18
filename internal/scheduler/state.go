package scheduler

import "fmt"

var transitions = map[string]map[string]bool{
	"queued":            {"waiting_resources": true, "starting": true, "cancelled": true, "failed": true},
	"waiting_resources": {"starting": true, "cancelling": true, "cancelled": true, "failed": true, "interrupted": true},
	"starting":          {"running": true, "failed": true, "cancelling": true, "interrupted": true},
	"running":           {"paused": true, "waiting_decision": true, "cancelling": true, "completed": true, "failed": true, "interrupted": true},
	"paused":            {"running": true, "cancelling": true, "failed": true, "interrupted": true},
	"waiting_decision":  {"running": true, "cancelling": true, "failed": true, "interrupted": true},
	"cancelling":        {"cancelled": true, "failed": true, "interrupted": true},
	"interrupted":       {"queued": true, "cancelled": true},
	"failed":            {"queued": true},
	"cancelled":         {"queued": true},
}

func ValidTransition(from, to string) bool {
	if from == to {
		return true
	}
	return transitions[from][to]
}
func ValidateTransition(from, to string) error {
	if !ValidTransition(from, to) {
		return fmt.Errorf("invalid run transition %s -> %s", from, to)
	}
	return nil
}
