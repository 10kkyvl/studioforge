package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/10kkyvl/studioforge/internal/providers"
)

// trackStudioMutation checks one raw provider event for a Studio MCP tool
// call that mutates the place currently open in Studio, and flags + persists
// the run the first time it sees one. It reuses the same two extractors
// trackStuckSignals does — toolUseNames for Claude's nested tool_use blocks,
// agentLoopToolName for the OpenRouter/NVIDIA agent loop's tool.call events —
// so every provider this scheduler runs is covered without a new payload
// walker.
//
// Called unconditionally from run()'s event loop, never gated on
// j.StuckDetectionEnabled the way trackStuckSignals is: an agent's per-run
// stuck-detection opt-out must not also silence the warning that it changed
// Studio directly, or the opt-out would create exactly the silent miss this
// exists to catch.
func (m *Manager) trackStudioMutation(e *execution, event providers.Event) {
	m.mu.Lock()
	checker := m.mutationChecker
	already := e.studioDirectEdits
	m.mu.Unlock()
	if checker == nil || already {
		return
	}
	var names []string
	switch event.Type {
	case "message":
		if isFullyBufferedMessage(event.RawType) {
			names = toolUseNames(event.Payload)
		}
	case "tool":
		if name := agentLoopToolName(event.RawType, event.Payload); name != "" {
			names = []string{name}
		}
	}
	mutated := false
	for _, name := range names {
		if checker(name) {
			mutated = true
			break
		}
	}
	if !mutated {
		return
	}
	m.mu.Lock()
	e.studioDirectEdits = true
	m.mu.Unlock()
	j := e.job
	// The tool call already reached Studio regardless of what happens to this
	// run next, so this is recorded on context.Background() rather than the
	// run's own cancellable ctx — the same reasoning the tail of run() uses for
	// its own post-completion writes: a Cancel landing this same instant must
	// never leave a mutation that already happened unrecorded.
	saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.store.SetRunStudioDirectEdits(saveCtx, j.RunID); err != nil {
		slog.Error("failed to persist studio-direct-edit flag", "run_id", j.RunID, "project_id", j.ProjectID, "error", err)
	}
}
