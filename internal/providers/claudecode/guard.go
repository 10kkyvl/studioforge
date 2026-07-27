package claudecode

import (
	"encoding/json"
	"io"

	"github.com/10kkyvl/studioforge/internal/providers/openrouter/agenttools"
)

// Guard is the PreToolUse hook StudioForge registers for a confined Claude run,
// run as `studioforge claude-guard --root <project>`.
//
// It reads one hook event from in, decides whether the path the tool was handed
// lands inside the project, and writes a decision to out. A path it cannot find
// or cannot judge is deferred to Claude Code's ordinary permission flow rather
// than denied: this narrows a run, and a guard that refused what it did not
// understand would break ordinary work while adding no safety.
//
// It never returns an error to its caller. A hook that exits non-zero is
// reported to the operator as a broken hook on every single tool call, which is
// worse than the gap it was trying to close.
func Guard(in io.Reader, out io.Writer, root string) {
	defer func() {
		// The guard sits in front of every file tool call. A panic here would
		// otherwise surface as a hook failure on each of them.
		_ = recover()
	}()

	var event struct {
		HookEventName string         `json:"hook_event_name"`
		ToolName      string         `json:"tool_name"`
		ToolInput     map[string]any `json:"tool_input"`
	}
	body, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil || json.Unmarshal(body, &event) != nil {
		return
	}
	path := toolPath(event.ToolInput)
	if path == "" {
		return
	}
	workspace, err := agenttools.NewWorkspace(root)
	if err != nil {
		// Without a resolvable project root there is nothing to measure against.
		return
	}
	if err := workspace.Contains(path); err != nil {
		writeDecision(out, "deny", event.ToolName+" was refused: "+err.Error()+". This run is confined to its project directory; work inside it.")
	}
}

// toolPath pulls the path a file tool was given. The key differs by tool and new
// tools appear, so an unrecognised shape yields nothing and the call is deferred
// rather than guessed at.
func toolPath(input map[string]any) string {
	for _, key := range []string{"file_path", "notebook_path", "path"} {
		if value, ok := input[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func writeDecision(out io.Writer, decision, reason string) {
	body, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       decision,
			"permissionDecisionReason": reason,
		},
	})
	if err != nil {
		return
	}
	_, _ = out.Write(body)
}
