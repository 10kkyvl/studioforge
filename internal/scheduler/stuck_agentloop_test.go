package scheduler

import (
	"testing"

	"github.com/10kkyvl/studioforge/internal/providers"
)

// The in-process agent loop (OpenRouter, NVIDIA) reports tool activity as
// tool.call / tool.result events with a flat payload, not as Claude's nested
// tool_use content blocks. Before this was handled, the repetition heuristic
// saw nothing at all on those providers even though the job carried
// StuckDetectionEnabled, so only the idle check could ever fire.
func agentLoopCall(name string) providers.Event {
	return providers.Event{Type: "tool", RawType: "tool.call", Payload: map[string]any{"tool": name, "arguments": "{}"}}
}

func agentLoopResult(name, text string) providers.Event {
	return providers.Event{Type: "tool", RawType: "tool.result", Payload: map[string]any{"tool": name, "result": text, "isError": false}}
}

func TestAgentLoopToolCallsFeedTheRepetitionHeuristic(t *testing.T) {
	m := &Manager{}
	j := &Job{RunID: "run-1", StuckRepetitionCap: 3}
	e := &execution{}
	// One cycle more than the cap: the heuristic requires the distinct
	// observation count to be unchanged across the whole repeated window, and
	// the very first result is itself a new observation.
	for range 4 {
		m.trackStuckSignals(e, agentLoopCall("start_stop_play"))
		m.trackStuckSignals(e, agentLoopResult("start_stop_play", "same console output"))
		m.trackStuckSignals(e, agentLoopCall("get_console_output"))
		m.trackStuckSignals(e, agentLoopResult("get_console_output", "same console output"))
	}
	reason, stuck := m.checkStuck(j, e)
	if !stuck {
		t.Fatalf("a repeated agent-loop tool cycle with no new observations should escalate; got reason=%q", reason)
	}
}

func TestAgentLoopWriteToolResetsTheRepetitionWindow(t *testing.T) {
	m := &Manager{}
	j := &Job{RunID: "run-1", StuckRepetitionCap: 3}
	e := &execution{}
	for range 3 {
		m.trackStuckSignals(e, agentLoopCall("start_stop_play"))
		m.trackStuckSignals(e, agentLoopCall("get_console_output"))
	}
	m.trackStuckSignals(e, agentLoopCall("apply_patch"))
	if len(e.toolCallsSinceEdit) != 0 {
		t.Fatalf("a write tool should clear the window, got %v", e.toolCallsSinceEdit)
	}
	if _, stuck := m.checkStuck(j, e); stuck {
		t.Fatal("a run that just edited a file is making progress and must not escalate")
	}
}

func TestAgentLoopNewObservationsKeepARepeatingCycleAlive(t *testing.T) {
	m := &Manager{}
	j := &Job{RunID: "run-1", StuckRepetitionCap: 3}
	e := &execution{}
	for i := range 3 {
		m.trackStuckSignals(e, agentLoopCall("start_stop_play"))
		m.trackStuckSignals(e, agentLoopCall("get_console_output"))
		m.trackStuckSignals(e, agentLoopResult("get_console_output", string(rune('a'+i))+" new error"))
	}
	if _, stuck := m.checkStuck(j, e); stuck {
		t.Fatal("a cycle that keeps turning up new console output is still learning, not stuck")
	}
}
