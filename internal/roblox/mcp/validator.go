package mcp

import (
	"context"
	"strings"
	"time"
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

// ValidateRequest is one automated playtest validation pass.
type ValidateRequest struct {
	Target Target
	// Window bounds how long the console is polled while in Play mode;
	// defaultValidateWindow is used when zero.
	Window time.Duration
	// PollInterval paces console polls within Window; defaultValidatePollInterval
	// is used when zero.
	PollInterval time.Duration
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
}

// Validate runs one automated playtest: enter Play mode, capture a
// screenshot, poll the console for Window, exit Play mode, and classify what
// the console said. It never fails a run — every early-exit path reports
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

	screenshot := ""
	if raw, err := client.Call(ctx, "screen_capture", nil); err == nil {
		if text, err := TextResult(raw); err == nil {
			screenshot = text
		}
	}

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

	outcome, errs := classifyConsole(console.String())
	result := ValidationResult{Outcome: outcome, Console: console.String(), Errors: errs, Screenshot: screenshot}
	if outcome == ValidationInconclusive {
		result.Notice = "playtest produced no console signal"
	}
	return result
}
