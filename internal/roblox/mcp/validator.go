package mcp

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/10kkyvl/studioforge/internal/attachments"
)

// ValidationOutcome is the result of one automated Play-mode validation pass.
type ValidationOutcome string

const (
	// ValidationNoErrors states what a clean pass actually established: no
	// script error appeared, and the place was confirmed to be running while
	// nothing appeared. It is deliberately not called "passed" — nobody played
	// the game, so a pass is not what was shown.
	ValidationNoErrors     ValidationOutcome = "no_errors_detected"
	ValidationFailed       ValidationOutcome = "failed"
	ValidationInconclusive ValidationOutcome = "inconclusive"
)

// Classification records how an outcome was reached, so the eventual accuracy
// of each route is measurable rather than assumed.
const (
	ClassifiedStructured = "structured"
	ClassifiedPhrases    = "phrases"
)

// errorMarkers are substrings that, case-insensitively, mark a console line as
// a script failure rather than ordinary game output. They mirror the failure
// shapes Roblox's own runtime and Luau VM print: an unhandled error, a
// dangling WaitForChild, or a missing member/index.
var errorMarkers = []string{
	"infinite yield",
	"attempt to index nil",
	"attempt to call a nil value",
	"is not a valid member of",
	"unhandled exception",
	"stack begin",
	" error:",
	"[error]",
}

// classifyConsole turns raw console text collected during a validation pass
// into an outcome, the specific lines that caused it, the parsed entries behind
// them, and which route reached the verdict.
//
// Empty output is inconclusive rather than a pass: silence is not the same as a
// clean run, and treating it as clean would let a Studio that never produced
// any signal look validated.
//
// Both routes run over every line, which is what a live Studio turned out to
// require. `get_console_output` returns bare message text: no timestamps, no
// severity, no message type. The only structural signal Roblox leaves is the
// attribution it puts on a runtime failure — "ServerScriptService.Main:12: …" —
// so that is what "structured" means here.
//
// Phrase matching is therefore not a fallback for text that failed to parse. It
// is the fallback for the errors Roblox prints with no attribution at all —
// "X is not a valid member of Y", "Infinite yield possible on …" — which are
// real, common, and carry nothing to key on but their wording. Treating an
// unattributed console as unparseable would have made a clean run permanently
// unprovable, since a console with no errors has nothing to attribute.
//
// Which route found the failures is what gets recorded; a console with none
// records nothing, because it was the Play-mode evidence, not either route, that
// decided the outcome.
func classifyConsole(text string) (ValidationOutcome, []string, []ConsoleEntry, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return ValidationInconclusive, nil, nil, ""
	}

	entries, _ := ParseConsole(text)
	var errs []string
	var failures []ConsoleEntry
	attributed := false
	for _, entry := range entries {
		switch {
		case entry.Failure():
			attributed = true
		case !entry.Graded && matchesErrorPhrase(entry.Message):
		default:
			continue
		}
		errs = append(errs, entry.Format())
		failures = append(failures, entry)
	}
	if len(errs) > 0 {
		if attributed {
			return ValidationFailed, errs, failures, ClassifiedStructured
		}
		return ValidationFailed, errs, failures, ClassifiedPhrases
	}
	return ValidationNoErrors, nil, nil, ""
}

// matchesErrorPhrase reports whether a message reads like one of the failures
// Roblox prints without attaching a script and line to it.
func matchesErrorPhrase(message string) bool {
	lower := strings.ToLower(message)
	for _, marker := range errorMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// defaultValidateWindow and defaultValidatePollInterval bound an automated
// playtest when a caller does not set ValidateRequest.Window/PollInterval.
const (
	defaultValidateWindow       = 30 * time.Second
	defaultValidatePollInterval = 3 * time.Second
)

// captureTimeout bounds the playtest's screenshot on its own, separately from
// the pass around it.
//
// It is generous because the tool is genuinely slow: a real capture measured
// about fifteen seconds to come back with a JPEG, and the same call has also
// been seen not to answer at all. Both need covering — too tight a bound turns
// a working capture into a miss, and no bound at all lets a wedged one hold the
// whole validation open, since this inherits the run's context. By the time the
// capture runs the console window has closed and the outcome is decided, so a
// capture that never returns costs the picture and nothing else.
const captureTimeout = 60 * time.Second

// captureID names the capture for Studio, which requires one. It is per call so
// two passes never collide over the same identifier.
func captureID() string {
	return "StudioForge_" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// ValidateRequest is one automated playtest validation pass.
type ValidateRequest struct {
	Target Target
	// Window bounds how long the console is polled while in Play mode;
	// defaultValidateWindow is used when zero.
	Window time.Duration
	// PollInterval paces console polls within Window; defaultValidatePollInterval
	// is used when zero.
	PollInterval time.Duration
	// ProjectPath is where a captured screenshot is stored, so the operator and
	// the correction run can both see it rather than being handed a path into
	// Studio's own world. Empty means the screenshot is recorded as whatever
	// text the tool returned, as it was before.
	ProjectPath string
}

// ValidationResult is the outcome of a Validate call: what the console said,
// which lines (if any) look like failures, and a reference to the one
// screenshot taken during Play mode.
type ValidationResult struct {
	Outcome    ValidationOutcome
	Console    string
	Errors     []string
	Screenshot string
	// Entries are the failing lines taken apart into script, line and message,
	// so the run view can show them as records instead of a wall of console
	// text. Empty when the console did not parse and phrase matching decided.
	Entries []ConsoleEntry
	// ClassifiedBy is which route reached the outcome, ClassifiedStructured or
	// ClassifiedPhrases, and empty when there was nothing to classify.
	ClassifiedBy string
	// Notice explains an Inconclusive result in terms an operator can act on
	// (Studio closed mid-playtest, no single instance, malformed responses).
	Notice string
	// Window is how long Play mode actually ran, after defaulting. It travels
	// with the result because the correction run that a failure schedules has to
	// describe the method that produced the finding, and by then the request
	// that set it is long gone.
	Window time.Duration
}

// capture takes the playtest's one screenshot and returns how the result should
// refer to it: a path inside the project's attachments when the image itself
// could be saved, so the operator and a correction run can both look at it, and
// otherwise whatever text the tool returned, which is all this used to have.
//
// Fail-open like everything else in the pass. A screenshot that cannot be taken,
// decoded or written costs the loop its visual signal; it must never be the
// reason a run is reported as failed.
func (p *Provisioner) capture(ctx context.Context, client *Client, projectPath string) string {
	// screen_capture takes a required capture_id, and calling it without one
	// fails outright with "Missing required argument" — which is what this call
	// did for as long as it has existed, so the loop's screenshot was always
	// empty and nothing downstream ever had one to show.
	//
	// It is also bounded separately from the rest of the pass: an observed
	// Studio can accept the call and never answer it, and a hung capture must
	// cost the visual signal rather than the whole validation, which has already
	// done its real work by this point.
	captureCtx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()
	raw, err := client.Call(captureCtx, "screen_capture", map[string]any{"capture_id": captureID()})
	if err != nil {
		return ""
	}
	if projectPath != "" {
		if image, err := ImageResult(raw); err == nil {
			if path, err := attachments.Save(projectPath, image); err == nil {
				return path
			}
		}
	}
	if text, err := TextResult(raw); err == nil {
		return text
	}
	return ""
}

// Validate runs one automated playtest: enter Play mode, poll the console for
// Window, capture a screenshot, exit Play mode, and classify what the console
// said. It never fails a run — every early-exit path reports
// Inconclusive with a Notice, mirroring Provision's own fail-open behavior,
// because a broken Studio connection here means "no signal", not "the
// playtest failed".
func (p *Provisioner) Validate(ctx context.Context, req ValidateRequest) ValidationResult {
	override := ""
	if p.Override != nil {
		override = p.Override()
	}
	launch, err := DetectLauncher(override)
	if err != nil {
		return ValidationResult{Outcome: ValidationInconclusive, Notice: "Studio MCP launcher not available: " + err.Error()}
	}
	instances, _, err := p.probe(ctx, launch)
	if err != nil {
		return ValidationResult{Outcome: ValidationInconclusive, Notice: "playtest validation withheld: " + err.Error()}
	}
	instances, _, notice := p.selectForTarget(ctx, launch, req.Target, instances, "")
	if notice != "" {
		return ValidationResult{Outcome: ValidationInconclusive, Notice: notice}
	}
	if len(instances) != 1 {
		return ValidationResult{Outcome: ValidationInconclusive, Notice: "playtest validation withheld: no single Studio instance is available"}
	}

	transport, err := p.dial(ctx, launch)
	if err != nil {
		return ValidationResult{Outcome: ValidationInconclusive, Notice: "playtest validation withheld: " + err.Error()}
	}
	client := NewClient(transport)
	defer func() { _ = client.Close() }()

	if _, err := client.Call(ctx, "start_stop_play", nil); err != nil {
		return ValidationResult{Outcome: ValidationInconclusive, Notice: "entering Play mode failed: " + err.Error()}
	}
	// Always try to leave Play mode as we found it, even if everything below
	// fails or the console never yields a usable signal.
	defer func() { _, _ = client.Call(ctx, "start_stop_play", nil) }()

	window := req.Window
	if window <= 0 {
		window = defaultValidateWindow
	}
	interval := req.PollInterval
	if interval <= 0 {
		interval = defaultValidatePollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	deadline := time.Now().Add(window)
	var console strings.Builder
	var lastConsoleText string
	playConfirmed := false
	for {
		if raw, err := client.Call(ctx, "get_console_output", nil); err == nil {
			if text, err := TextResult(raw); err == nil && text != "" {
				// Studio answers every poll with the whole console buffer, not the
				// delta since the last one, so appending it wholesale here would
				// make Console grow quadratically with the number of polls.
				// Shift by the common prefix instead: if the new text starts with
				// what the previous poll already contributed, only the remainder
				// is new and gets appended; otherwise the buffer rotated or was
				// truncated and the whole thing is kept so nothing is lost. This
				// is deliberately not a line-set dedup — a line that genuinely
				// repeats within one buffer is real output and must survive,
				// which a line-set dedup would silently collapse. dedupeEntries
				// (console.go) takes the opposite tradeoff on purpose for
				// Errors: it collapses repeats there because that list feeds a
				// correction prompt, where a repeat is pure noise rather than
				// evidence.
				if strings.HasPrefix(text, lastConsoleText) {
					console.WriteString(text[len(lastConsoleText):])
				} else {
					if console.Len() > 0 {
						console.WriteString("\n")
					}
					console.WriteString(text)
				}
				lastConsoleText = text
			}
		}
		// Ask Studio what mode it is actually in, until it says Play. Entering
		// Play mode is not instant, so a single early check would report Edit on
		// a session that is about to run — but once it has been confirmed once,
		// there is nothing left to establish and the call stops.
		if !playConfirmed {
			if raw, err := client.Call(ctx, "get_studio_state", nil); err == nil {
				playConfirmed = parsePlayState(studioStateText(raw)) == "play"
			}
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}

	// Captured here, at the end of the window rather than the start: a shot
	// taken the instant Play mode was entered is of a place that has not
	// finished loading, which is why the one visual signal the whole loop
	// collects used to show a loading screen or an empty baseplate. This is the
	// last thing before the deferred start_stop_play takes the session out of
	// Play mode.
	screenshot := p.capture(ctx, client, req.ProjectPath)

	outcome, errs, entries, classifiedBy := classifyConsole(console.String())
	// A clean console is only worth reporting as such if the place was seen
	// running. Without that, "no errors appeared" is indistinguishable from
	// "nothing ran", and the second is exactly the case an operator must not be
	// told looks fine. A failure needs no such confirmation: errors that
	// appeared, appeared.
	if outcome == ValidationNoErrors && !playConfirmed {
		outcome = ValidationInconclusive
	}
	result := ValidationResult{
		Outcome:      outcome,
		Console:      console.String(),
		Errors:       errs,
		Entries:      entries,
		ClassifiedBy: classifiedBy,
		Screenshot:   screenshot,
		Window:       window,
	}
	switch {
	case outcome != ValidationInconclusive:
	case classifiedBy == "":
		result.Notice = "playtest produced no console signal"
	case !playConfirmed:
		result.Notice = "playtest could not confirm the place entered Play mode, so a clean console proves nothing"
	default:
		result.Notice = "playtest console output could not be parsed, so no absence of errors was established"
	}
	return result
}
