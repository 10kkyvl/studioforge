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
	ValidationPassed       ValidationOutcome = "passed"
	ValidationFailed       ValidationOutcome = "failed"
	ValidationInconclusive ValidationOutcome = "inconclusive"
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
// into an outcome and the specific lines that caused it. Empty output is
// inconclusive rather than a pass: silence is not the same as a clean run,
// and treating it as "passed" would let a Studio that never produced any
// signal (e.g. the place never actually entered Play mode) look validated.
func classifyConsole(text string) (ValidationOutcome, []string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return ValidationInconclusive, nil
	}
	var errs []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		for _, marker := range errorMarkers {
			if strings.Contains(lower, marker) {
				errs = append(errs, line)
				break
			}
		}
	}
	if len(errs) > 0 {
		return ValidationFailed, errs
	}
	return ValidationPassed, nil
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
	for {
		if raw, err := client.Call(ctx, "get_console_output", nil); err == nil {
			if text, err := TextResult(raw); err == nil && text != "" {
				console.WriteString(text)
				console.WriteString("\n")
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

	outcome, errs := classifyConsole(console.String())
	result := ValidationResult{Outcome: outcome, Console: console.String(), Errors: errs, Screenshot: screenshot, Window: window}
	if outcome == ValidationInconclusive {
		result.Notice = "playtest produced no console signal"
	}
	return result
}
