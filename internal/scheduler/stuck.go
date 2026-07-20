package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers"
)

// StuckContinueLabel is the exact option label a stuck-escalation question
// offers for "keep going". api.createRun compares an incoming message against
// this exact constant (never a duplicated string literal) to decide whether a
// resume is a "continue" that should suppress stuck detection entirely for
// the resumed run — the operator explicitly said keep going, so asking again
// on the same run would only nag — or anything else (a genuine typed
// clarification) that leaves detection enabled at the current settings.
const StuckContinueLabel = "Continue testing"

// StuckSettings is the resolved global stuck-detection configuration, read
// once per run submission the same way ValidateAfterRun/MaxCorrectionRuns
// are: internal/app wires the live settings (playtest_window_seconds' own
// atomic.Int64/atomic.Bool pattern) behind a closure, and internal/api calls
// it right where it already resolves the rest of a new Job's fields, before
// Submit.
type StuckSettings struct {
	Enabled       bool
	IdleSeconds   int
	RepetitionCap int
}

// stuckFileEditTools are Claude's own built-in tools that change a file on
// disk, as opposed to a Studio MCP tool (start_stop_play, get_console_output,
// and the rest) that only observes or drives an already-open Studio. Seeing
// one of these means the run made real progress, so the repetition heuristic
// resets rather than counting it as part of a stuck loop.
var stuckFileEditTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true}

func isFileEditTool(name string) bool { return stuckFileEditTools[name] }

// toolUseNames pulls every tool_use content block's "name" out of a fully
// buffered Claude assistant message's payload, in the order Claude emitted
// them (a single message can carry more than one tool call). Mirrors
// messageText's own decode-and-dig-in style so the two stay easy to compare;
// unlike messageText it looks at type=="tool_use" blocks instead of
// type=="text" ones. Any other provider's payload shape (or a shape this
// message simply lacks) yields nil, not an error.
func toolUseNames(payload any) []string {
	decoded, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	message, ok := decoded["message"].(map[string]any)
	if !ok {
		return nil
	}
	content, ok := message["content"].([]any)
	if !ok {
		return nil
	}
	var names []string
	for _, entryAny := range content {
		entry, ok := entryAny.(map[string]any)
		if !ok || entry["type"] != "tool_use" {
			continue
		}
		if name, ok := entry["name"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	return names
}

// toolResultText pulls the human-readable text out of a Claude "user"-role
// stream event (Type "tool" after normalize()), which carries the result of
// a tool call as one or more tool_result content blocks. A tool_result's own
// "content" is either a bare string or an array of blocks (per the Anthropic
// API); both shapes are handled. Used only to tell whether a repeating tool
// call is turning up the same observation every time (stuck) or something
// new (still learning), and to fill the escalation message's "what it has
// been seeing" section.
func toolResultText(payload any) string {
	decoded, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	message, ok := decoded["message"].(map[string]any)
	if !ok {
		return ""
	}
	content, ok := message["content"].([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, entryAny := range content {
		entry, ok := entryAny.(map[string]any)
		if !ok || entry["type"] != "tool_result" {
			continue
		}
		switch value := entry["content"].(type) {
		case string:
			if value != "" {
				parts = append(parts, value)
			}
		case []any:
			for _, blockAny := range value {
				block, ok := blockAny.(map[string]any)
				if !ok || block["type"] != "text" {
					continue
				}
				if text, ok := block["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// maxStuckCycleLen bounds how long a repeated tool-call cycle detectRepeatedCycle
// looks for. Deliberately a small fixed constant, not a setting: the point is
// catching a short back-and-forth (e.g. start_stop_play, get_console_output,
// stop_play repeating), not modeling arbitrarily long behavior.
const maxStuckCycleLen = 4

// maxStuckToolHistory bounds toolCallsSinceEdit/obsCountAtToolCall so a
// pathologically long-running job without a single file edit cannot grow
// these slices without bound; comfortably larger than
// maxStuckCycleLen*any sane stuck_repetition_cap.
const maxStuckToolHistory = 500

// maxStuckObservations caps how many distinct console/tool-result
// observations the escalation message quotes, so it stays short.
const maxStuckObservations = 5

// detectRepeatedCycle reports whether names ends with repCap consecutive,
// exactly-equal repeats of some short cycle length L (checked for L from 1 to
// maxStuckCycleLen, smallest first). For example
// ["A","B","A","B","A","B"] with repCap=3 matches at L=2. Returns the L that
// matched.
func detectRepeatedCycle(names []string, repCap int) (cycleLen int, ok bool) {
	if repCap < 2 {
		return 0, false
	}
	n := len(names)
	for l := 1; l <= maxStuckCycleLen; l++ {
		need := l * repCap
		if n < need {
			continue
		}
		window := names[n-need:]
		first := window[:l]
		matched := true
		for c := 1; c < repCap && matched; c++ {
			chunk := window[c*l : (c+1)*l]
			for i := range first {
				if chunk[i] != first[i] {
					matched = false
					break
				}
			}
		}
		if matched {
			return l, true
		}
	}
	return 0, false
}

// stuckIdleReason is the idle stuck check: a running job that has not
// produced a single provider event — no streamed text, no tool call, no tool
// result — for longer than the limit is treated as hung. Any event at all
// resets the anchor this measures against, so an actively working run can
// never trip it no matter how long it runs.
func stuckIdleReason(idle time.Duration, idleSeconds int) (string, bool) {
	if idleSeconds <= 0 {
		return "", false
	}
	limit := time.Duration(idleSeconds) * time.Second
	if idle <= limit {
		return "", false
	}
	return fmt.Sprintf("It has produced no output, tool calls or results for %s, past the %s idle limit.", idle.Round(time.Second), limit), true
}

// stuckRepetitionReason is the repeated-tool-cycle stuck check: toolNames and
// obsCounts are the tool-call names and their paired distinct-observation
// snapshots since the last file edit (see execution.toolCallsSinceEdit).
// Repetition alone is not enough — it also requires the distinct-observation
// count to be unchanged across the whole repeated window, i.e. nothing newly
// distinct (a different console error, a different playtest result) turned
// up anywhere in it. If new information kept appearing, the agent is still
// learning something each cycle, not stuck.
func stuckRepetitionReason(toolNames []string, obsCounts []int, repCap int) (string, bool) {
	if repCap <= 0 || len(obsCounts) != len(toolNames) {
		return "", false
	}
	cycleLen, ok := detectRepeatedCycle(toolNames, repCap)
	if !ok {
		return "", false
	}
	need := cycleLen * repCap
	n := len(obsCounts)
	if obsCounts[n-1] != obsCounts[n-need] {
		return "", false
	}
	pattern := strings.Join(toolNames[n-cycleLen:], " -> ")
	return fmt.Sprintf("The same %d-call tool sequence (%s) has repeated %d times in a row with no file edit and no new console output.", cycleLen, pattern, repCap), true
}

// buildStuckMessage assembles the escalation message's text: a short framing
// paragraph (what the run was asked to do, why StudioForge paused it, and
// what it has recently observed), a direct question, and the
// ```studioforge-question fence itself — the exact same fenced-JSON contract
// a coding agent's own natural question uses, with a single "continue"
// option. Reused end to end by the existing question machinery: the question
// fence is inside a normal message event's text, so detectQuestion,
// extractQuestionFence, and the waiting_decision transition all already know
// how to handle it with no changes of their own.
func buildStuckMessage(j *Job, reason string, observations []string) string {
	var b strings.Builder
	b.WriteString("StudioForge paused this run to check in before it keeps going.\n\n")
	if prompt := strings.TrimSpace(firstLine(j.Prompt)); prompt != "" {
		b.WriteString("What it was asked to do: " + truncate(prompt, 200) + "\n")
	}
	b.WriteString(reason + "\n")
	if len(observations) > 0 {
		b.WriteString("\nRecent console/playtest observations:\n")
		for _, observation := range observations {
			b.WriteString("- " + truncate(observation, 200) + "\n")
		}
	}
	b.WriteString("\nReply to redirect it, or choose an option below.\n\n")
	block := questionBlock{
		Question: "This run looks stuck. Continue, or should it stop here?",
		Options:  []questionOption{{Label: StuckContinueLabel, Description: "Resume the same session and keep going."}},
	}
	// questionBlock only ever holds strings, so this can never fail.
	encoded, _ := json.Marshal(block)
	b.WriteString("```studioforge-question\n")
	b.Write(encoded)
	b.WriteString("\n```")
	return b.String()
}

// trackStuckSignals updates an execution's repetition-heuristic bookkeeping
// from one raw provider event: a fully buffered assistant message's tool_use
// blocks either reset the "since last edit" window (a file-edit tool call)
// or extend it (any other tool, paired with how many distinct observations
// have been seen so far), and a tool-result event folds its text into the
// distinct-observations set. A no-op for any provider or event shape that
// does not carry these Claude-specific fields.
func (m *Manager) trackStuckSignals(e *execution, event providers.Event) {
	switch event.Type {
	case "message":
		if !isFullyBufferedMessage(event.RawType) {
			return
		}
		for _, name := range toolUseNames(event.Payload) {
			if isFileEditTool(name) {
				e.toolCallsSinceEdit = nil
				e.obsCountAtToolCall = nil
				e.distinctObservations = nil
				e.recentObservations = nil
				continue
			}
			e.toolCallsSinceEdit = append(e.toolCallsSinceEdit, name)
			e.obsCountAtToolCall = append(e.obsCountAtToolCall, len(e.distinctObservations))
			if len(e.toolCallsSinceEdit) > maxStuckToolHistory {
				trim := len(e.toolCallsSinceEdit) - maxStuckToolHistory
				e.toolCallsSinceEdit = e.toolCallsSinceEdit[trim:]
				e.obsCountAtToolCall = e.obsCountAtToolCall[trim:]
			}
		}
	case "tool":
		text := toolResultText(event.Payload)
		if text == "" {
			return
		}
		if e.distinctObservations == nil {
			e.distinctObservations = map[string]bool{}
		}
		if e.distinctObservations[text] {
			return
		}
		e.distinctObservations[text] = true
		e.recentObservations = append(e.recentObservations, text)
		if len(e.recentObservations) > maxStuckObservations {
			e.recentObservations = e.recentObservations[len(e.recentObservations)-maxStuckObservations:]
		}
	}
}

// checkStuck evaluates the per-event repetition threshold against an
// execution's current state; the idle threshold lives on the run loop's
// ticker instead, since a hung provider delivers no event to evaluate on.
func (m *Manager) checkStuck(j *Job, e *execution) (string, bool) {
	return stuckRepetitionReason(e.toolCallsSinceEdit, e.obsCountAtToolCall, j.StuckRepetitionCap)
}

// escalateStuck stops the current provider turn exactly like the existing
// cancel/lease-lost paths in run() do (handle.Cancel() then handle.Wait(),
// usage recorded on context.Background()), publishes the escalation message,
// and transitions the run to waiting_decision with its stuck_escalated flag
// recorded. Called from inside run()'s own goroutine; the
// caller returns immediately afterward, the same shape as the existing
// asksQuestion tail exit.
//
// handle.Cancel()/handle.Wait() can take real wall-clock time (the run stays
// in m.active for all of it), so a Manager.Cancel can race in while this is
// still mid-flight: ctx is re-checked right after handle.Wait() returns, the
// same checkpoint the run loop's own ctx.Done()/natural-completion paths
// already use. If a Cancel won that race, this run must land on cancelled
// like every sibling termination path does, not waiting_decision — so the
// stuck message and transitionStuck (which would otherwise write
// waiting_decision unconditionally) are skipped entirely in favor of the same
// cancelling->cancelled sequence those sibling paths use.
func (m *Manager) escalateStuck(ctx context.Context, j *Job, e *execution, handle providers.RunHandle, reason string) {
	handle.Cancel()
	result := handle.Wait()
	_ = m.store.SetRunUsage(context.Background(), j.RunID, result.SessionID, result.Cost, models.TokenUsage(result.Usage))
	if ctx.Err() != nil {
		m.transition(context.Background(), j, "running", "cancelling", "cancelling", "", "")
		m.transition(context.Background(), j, "cancelling", "cancelled", "cancelled", "", "")
		return
	}
	text := buildStuckMessage(j, reason, e.recentObservations)
	m.emit(models.Run{ID: j.RunID, ProjectID: j.ProjectID}, j.AgentID, "message", "scheduler.stuck", map[string]any{"text": text})
	m.transitionStuck(context.Background(), j)
}

// transitionStuck is transition()'s running->waiting_decision case, except it
// writes through UpdateRunStuck instead of UpdateRun so the stuck_escalated
// flag lands in the exact same write as the status change.
func (m *Manager) transitionStuck(ctx context.Context, j *Job) {
	const from, to, phase = "running", "waiting_decision", "waiting_decision"
	if err := ValidateTransition(from, to); err != nil {
		m.emit(models.Run{ID: j.RunID, ProjectID: j.ProjectID}, j.AgentID, "error", "scheduler.transition", map[string]any{"message": err.Error()})
		return
	}
	if err := m.store.UpdateRunStuck(ctx, j.RunID, to, phase, "", ""); err != nil {
		slog.Error("failed to persist stuck-escalation run transition", "run_id", j.RunID, "status", to, "error", err)
	}
	m.emit(models.Run{ID: j.RunID, ProjectID: j.ProjectID}, j.AgentID, "status", "scheduler.state", map[string]any{"status": to, "phase": phase, "resource": ""})
}
