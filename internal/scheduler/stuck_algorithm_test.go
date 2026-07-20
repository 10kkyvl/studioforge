package scheduler

import (
	"strings"
	"testing"
	"time"
)

// TestDetectRepeatedCycle exercises the exact algorithm documented in
// stuck.go: does the tail of names consist of repCap consecutive,
// exactly-equal repeats of some cycle length from 1 to maxStuckCycleLen
// (smallest first)?
func TestDetectRepeatedCycle(t *testing.T) {
	cases := []struct {
		name    string
		names   []string
		repCap  int
		wantLen int
		wantOK  bool
	}{
		{"too short for any repeat", []string{"A", "B"}, 3, 0, false},
		{"repCap below 2 never matches", []string{"A", "A", "A"}, 1, 0, false},
		{"single-name cycle repeats exactly", []string{"A", "A", "A"}, 3, 1, true},
		{"two-name cycle repeats exactly", []string{"A", "B", "A", "B", "A", "B"}, 3, 2, true},
		{"a longer, unrelated prefix does not block a matching tail", []string{"X", "Y", "Z", "A", "B", "A", "B", "A", "B"}, 3, 2, true},
		{"a near match that breaks on the last element does not count", []string{"A", "B", "A", "B", "A", "C"}, 3, 0, false},
		{"three-name cycle repeats exactly", []string{"A", "B", "C", "A", "B", "C"}, 2, 3, true},
		{"a cycle longer than maxStuckCycleLen is not detected", []string{"A", "B", "C", "D", "E", "A", "B", "C", "D", "E"}, 2, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotLen, gotOK := detectRepeatedCycle(tc.names, tc.repCap)
			if gotOK != tc.wantOK || (tc.wantOK && gotLen != tc.wantLen) {
				t.Errorf("detectRepeatedCycle(%v, %d) = (%d, %v), want (%d, %v)", tc.names, tc.repCap, gotLen, gotOK, tc.wantLen, tc.wantOK)
			}
		})
	}
}

func TestStuckIdleReason(t *testing.T) {
	cases := []struct {
		name        string
		idle        time.Duration
		idleSeconds int
		wantOK      bool
	}{
		{"disabled limit (0) never trips", 10 * time.Hour, 0, false},
		{"idle under the limit does not trip", 500 * time.Second, 600, false},
		{"idle exactly at the limit does not trip", 600 * time.Second, 600, false},
		{"idle past the limit trips", 700 * time.Second, 600, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := stuckIdleReason(tc.idle, tc.idleSeconds)
			if ok != tc.wantOK {
				t.Errorf("stuckIdleReason(%v, %d) ok=%v, want %v", tc.idle, tc.idleSeconds, ok, tc.wantOK)
			}
			if ok && !strings.Contains(reason, "idle limit") {
				t.Errorf("reason=%q must name the idle limit", reason)
			}
		})
	}
}

func TestStuckRepetitionReason(t *testing.T) {
	names := []string{"start_stop_play", "get_console_output", "start_stop_play", "get_console_output", "start_stop_play", "get_console_output"}
	t.Run("repeats with unchanged distinct-observation count trips", func(t *testing.T) {
		obs := []int{1, 1, 1, 1, 1, 1}
		reason, ok := stuckRepetitionReason(names, obs, 3)
		if !ok {
			t.Fatal("expected repetition to trip")
		}
		if !strings.Contains(reason, "start_stop_play") || !strings.Contains(reason, "get_console_output") {
			t.Errorf("reason=%q must name the repeating tools", reason)
		}
	})
	t.Run("repeats with new distinct observations mid-window do not trip", func(t *testing.T) {
		// The distinct-observation count grew between the window's start and
		// its end, so something new was learned each cycle — not stuck.
		obs := []int{1, 1, 1, 2, 2, 3}
		if _, ok := stuckRepetitionReason(names, obs, 3); ok {
			t.Error("new distinct observations during the window must prevent the trip")
		}
	})
	t.Run("mismatched slice lengths never trip", func(t *testing.T) {
		if _, ok := stuckRepetitionReason(names, []int{1, 1}, 3); ok {
			t.Error("a torn/mismatched bookkeeping pair must never trip")
		}
	})
	t.Run("disabled cap never trips", func(t *testing.T) {
		if _, ok := stuckRepetitionReason(names, []int{1, 1, 1, 1, 1, 1}, 0); ok {
			t.Error("repCap<=0 must disable the check")
		}
	})
}

// TestCheckStuckFalsePositives checks the per-event check — now repetition
// only — trips nothing for a short, unremarkable run's bookkeeping.
func TestCheckStuckFalsePositives(t *testing.T) {
	m := &Manager{}
	j := &Job{StuckRepetitionCap: 6}
	e := &execution{toolCallsSinceEdit: []string{"Read", "Bash", "get_console_output"}, obsCountAtToolCall: []int{0, 1, 2}}
	if reason, ok := m.checkStuck(j, e); ok {
		t.Errorf("a varied, learning run must not trip, got %q", reason)
	}
}

// TestBuildStuckMessageProducesAValidQuestionFence checks the escalation
// message's fence is exactly what detectQuestion already knows how to parse,
// with the single "continue" option and no "stop"/"clarify" options baked
// into the fence's JSON contract.
func TestBuildStuckMessageProducesAValidQuestionFence(t *testing.T) {
	j := &Job{Prompt: "Playtest the new obstacle course and report back.\nSecond line ignored."}
	text := buildStuckMessage(j, "It has produced no output, tool calls or results for 12m0s, past the 10m0s idle limit.", []string{"attempt to index nil", "attempt to index nil"})
	block, ok := detectQuestion(text)
	if !ok {
		t.Fatalf("buildStuckMessage produced a fence detectQuestion could not parse: %q", text)
	}
	if len(block.Options) != 2 || block.Options[0].Label != StuckContinueLabel || block.Options[1].Label != StuckStopLabel {
		t.Errorf("options=%+v, want %q then %q", block.Options, StuckContinueLabel, StuckStopLabel)
	}
	if !strings.Contains(text, "Playtest the new obstacle course and report back.") {
		t.Error("message must mention what the run was asked to do")
	}
	if strings.Contains(text, "Second line ignored") {
		t.Error("only the prompt's first line belongs in the message")
	}
}
