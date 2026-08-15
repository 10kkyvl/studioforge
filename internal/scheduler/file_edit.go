package scheduler

import (
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers"
)

// trackFileEdit publishes a transient scheduler.file_edit run event whenever
// one raw provider event carries a file-edit tool call, so the UI can show
// "the agent is editing" live without waiting on the run's own persisted
// timeline. It reuses the same two extractors trackStuckSignals and
// trackStudioMutation both already use — toolUseNames for Claude's nested
// tool_use content blocks, agentLoopToolName for the OpenRouter/NVIDIA agent
// loop's tool.call events — so this adds no second payload walker, and
// isFileEditTool for the shared file-edit tool list, so the set of tools
// that count never drifts from what stuck detection already treats as real
// progress.
//
// Called unconditionally from run()'s event loop, the same way
// trackStudioMutation is: which tools were just called is a fact about what
// happened, independent of the per-run stuck-detection opt-out.
func (m *Manager) trackFileEdit(e *execution, event providers.Event) {
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
	var editNames []string
	for _, name := range names {
		if isFileEditTool(name) {
			editNames = append(editNames, name)
		}
	}
	if len(editNames) == 0 {
		return
	}
	m.mu.Lock()
	e.fileEdited = true
	m.mu.Unlock()
	j := e.job
	m.hub.PublishTransient(models.RunEvent{
		ProjectID: j.ProjectID,
		RunID:     j.RunID,
		AgentID:   j.AgentID,
		Type:      "file_edit",
		RawType:   "scheduler.file_edit",
		Payload:   map[string]any{"tools": editNames},
		CreatedAt: time.Now().UTC(),
	})
}
