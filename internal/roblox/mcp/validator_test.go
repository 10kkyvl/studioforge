package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClassifyConsoleWithNoOutputIsInconclusive(t *testing.T) {
	outcome, errs, _, by := classifyConsole("")
	if outcome != ValidationInconclusive {
		t.Fatalf("outcome=%q, want inconclusive", outcome)
	}
	if len(errs) != 0 {
		t.Errorf("errs=%v, want none", errs)
	}
	if by != "" {
		t.Errorf("classifiedBy=%q, want empty — there was nothing to classify", by)
	}
}

func TestClassifyConsoleWithOnlyOrdinaryStructuredOutputFindsNoErrors(t *testing.T) {
	outcome, errs, _, by := classifyConsole("[Info] Server started\n[Output] Player joined the game\n[Output] Round 1 begins")
	if outcome != ValidationNoErrors {
		t.Fatalf("outcome=%q, want no_errors_detected", outcome)
	}
	if len(errs) != 0 {
		t.Errorf("errs=%v, want none", errs)
	}
	if by != "" {
		t.Errorf("classifiedBy=%q, want empty — neither route found anything, so neither decided", by)
	}
}

// A live Studio returns bare message text with no severity and no timestamps, so
// a clean console has nothing to attribute and nothing to grade. Treating that
// as unparseable would make a clean run permanently unprovable. What stops it
// being reported as a pass is the Play-mode evidence in Validate, not the
// classifier — see TestValidateWithoutPlayModeEvidenceIsNotPassLike.
func TestClassifyConsoleWithPlainCleanOutputFindsNoErrors(t *testing.T) {
	outcome, errs, _, by := classifyConsole("Server started\nPlayer joined the game\nRound 1 begins")
	if outcome != ValidationNoErrors {
		t.Fatalf("outcome=%q, want no_errors_detected", outcome)
	}
	if len(errs) != 0 {
		t.Errorf("errs=%v, want none", errs)
	}
	if by != "" {
		t.Errorf("classifiedBy=%q, want empty", by)
	}
}

// Roblox prints these two with no script and no line, so nothing but their
// wording identifies them. Observed live: "NoSuchChildHere is not a valid member
// of Workspace \"Workspace\"" carries no attribution at all.
func TestClassifyConsoleCatchesUnattributedRobloxErrors(t *testing.T) {
	for _, line := range []string{
		`NoSuchChildHere is not a valid member of Workspace "Workspace"`,
		"Infinite yield possible on 'ReplicatedStorage:WaitForChild(\"Remote\")'",
	} {
		outcome, errs, _, by := classifyConsole("Server started\n" + line)
		if outcome != ValidationFailed {
			t.Errorf("%q -> %q, want failed", line, outcome)
		}
		if len(errs) != 1 {
			t.Errorf("%q -> errs=%v, want one", line, errs)
		}
		if by != ClassifiedPhrases {
			t.Errorf("%q -> classifiedBy=%q, want phrases — Roblox attributes neither", line, by)
		}
	}
}

// The shape a live Studio actually returns for a runtime failure, captured by
// TestRealStudioErrorShape: script, line, message, and nothing else.
func TestClassifyConsoleReadsTheLiveErrorShape(t *testing.T) {
	outcome, errs, entries, by := classifyConsole("AssistantCommand:9: attempt to index nil with 'field'")
	if outcome != ValidationFailed {
		t.Fatalf("outcome=%q, want failed", outcome)
	}
	if by != ClassifiedStructured {
		t.Errorf("classifiedBy=%q, want structured — the attribution is the signal", by)
	}
	if len(entries) != 1 || entries[0].Script != "AssistantCommand" || entries[0].Line != 9 {
		t.Errorf("entries=%+v", entries)
	}
	if len(errs) != 1 {
		t.Errorf("errs=%v", errs)
	}
}

func TestClassifyConsoleDetectsAScriptError(t *testing.T) {
	outcome, errs, entries, by := classifyConsole("Server started\nServerScriptService.Main:12: attempt to index nil with 'Humanoid'\nRound 1 begins")
	if outcome != ValidationFailed {
		t.Fatalf("outcome=%q, want failed", outcome)
	}
	if !reflect.DeepEqual(errs, []string{"ServerScriptService.Main:12 — attempt to index nil with 'Humanoid'"}) {
		t.Errorf("errs=%v", errs)
	}
	if by != ClassifiedStructured {
		t.Errorf("classifiedBy=%q, want structured", by)
	}
	if len(entries) != 1 || entries[0].Script != "ServerScriptService.Main" || entries[0].Line != 12 {
		t.Errorf("entries=%+v, want one entry attributed to ServerScriptService.Main:12", entries)
	}
}

func TestClassifyConsoleDetectsInfiniteYield(t *testing.T) {
	outcome, errs, _, _ := classifyConsole("[Warning] Infinite yield possible on 'ReplicatedStorage:WaitForChild(\"Remote\")'")
	if outcome != ValidationFailed {
		t.Fatalf("outcome=%q, want failed", outcome)
	}
	if len(errs) != 1 {
		t.Fatalf("errs=%v, want one line", errs)
	}
}

func TestClassifyConsoleCollectsMultipleErrorLines(t *testing.T) {
	outcome, errs, _, by := classifyConsole("attempt to call a nil value\nfine line\nX is not a valid member of Instance")
	if outcome != ValidationFailed {
		t.Fatalf("outcome=%q, want failed", outcome)
	}
	if len(errs) != 2 {
		t.Fatalf("errs=%v, want two lines", errs)
	}
	if by != ClassifiedPhrases {
		t.Errorf("classifiedBy=%q, want phrases — none of those lines carry a grade or a script", by)
	}
}

// A wording that sounds alarming in ordinary output is what phrase matching gets
// wrong in the other direction, and what a severity grade settles.
func TestClassifyConsoleDoesNotFailOnAlarminglyWordedOrdinaryOutput(t *testing.T) {
	outcome, _, _, by := classifyConsole("[Output] Loaded error handler module\n[Output] recovering from unhandled exception in the queue")
	if outcome != ValidationNoErrors {
		t.Fatalf("outcome=%q, want no_errors_detected", outcome)
	}
	if by != "" {
		t.Errorf("classifiedBy=%q, want empty — nothing was found, so nothing decided", by)
	}
	// The grade is what protects it: the same wording ungraded still matches.
	if outcome, _, _, by := classifyConsole("recovering from unhandled exception in the queue"); outcome != ValidationFailed || by != ClassifiedPhrases {
		t.Errorf("ungraded, the same wording must still be caught: outcome=%q by=%q", outcome, by)
	}
}

// Studio answers every poll with the whole buffer, so one error arrives once per
// remaining tick. The operator should be shown it once.
func TestClassifyConsoleCollapsesOutputRepeatedAcrossPolls(t *testing.T) {
	one := "[Output] Server started\nServerScriptService.Main:12: attempt to index nil with 'Humanoid'\n"
	outcome, errs, _, _ := classifyConsole(one + one + one)
	if outcome != ValidationFailed {
		t.Fatalf("outcome=%q, want failed", outcome)
	}
	if len(errs) != 1 {
		t.Fatalf("errs=%v, want the error reported once rather than once per poll", errs)
	}
}

// playtestTransport scripts start_stop_play/get_console_output/screen_capture
// for validator tests, on top of studioTransport's list_roblox_studios
// support (needed so Validate's own instance-selection reuses selectForTarget
// exactly like Provision does).
type playtestTransport struct {
	studioTransport
	consoleResponses []string // one per get_console_output call; last repeats once exhausted
	consoleErr       error
	malformedConsole bool
	screenshotText   string
	closeAfterCalls  int // once total Calls exceeds this, every call errors; 0 = never
	// editModeOnly makes get_studio_state keep reporting Edit, standing in for a
	// session that never actually entered Play mode.
	editModeOnly bool

	totalCalls      int
	playCalls       int
	consoleCalls    int
	screenshotCalls int
	stateCalls      int
}

func (p *playtestTransport) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	p.totalCalls++
	if p.closeAfterCalls > 0 && p.totalCalls > p.closeAfterCalls {
		return nil, errors.New("Studio MCP transport closed")
	}
	switch name {
	case "start_stop_play":
		p.playCalls++
		return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
	case "get_console_output":
		p.consoleCalls++
		if p.consoleErr != nil {
			return nil, p.consoleErr
		}
		if p.malformedConsole {
			return json.RawMessage(`not valid json`), nil
		}
		text := ""
		switch {
		case len(p.consoleResponses) == 0:
		case p.consoleCalls-1 < len(p.consoleResponses):
			text = p.consoleResponses[p.consoleCalls-1]
		default:
			text = p.consoleResponses[len(p.consoleResponses)-1]
		}
		body, err := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}})
		if err != nil {
			return nil, err
		}
		return body, nil
	case "get_studio_state":
		p.stateCalls++
		state := "Studio is in Play mode"
		if p.editModeOnly {
			state = "Studio is in Edit mode"
		}
		body, err := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": state}}})
		if err != nil {
			return nil, err
		}
		return body, nil
	case "screen_capture":
		p.screenshotCalls++
		body, err := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": p.screenshotText}}})
		if err != nil {
			return nil, err
		}
		return body, nil
	default:
		return p.studioTransport.Call(ctx, name, args)
	}
}

// fastValidateRequest keeps validator tests from actually sleeping through a
// real polling window.
func fastValidateRequest() ValidateRequest {
	return ValidateRequest{Window: 20 * time.Millisecond, PollInterval: 5 * time.Millisecond}
}

func TestValidateReportsNoErrorsWhenConsoleIsCleanAndPlayModeIsConfirmed(t *testing.T) {
	transport := &playtestTransport{
		studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		consoleResponses: []string{"[Output] Server started", "[Output] Player joined"},
		screenshotText:   "C:\\shots\\1.png",
	}
	p := newProvisioner(t, transport)
	result := p.Validate(context.Background(), fastValidateRequest())
	if result.Outcome != ValidationNoErrors {
		t.Fatalf("outcome=%q notice=%q, want no_errors_detected", result.Outcome, result.Notice)
	}
	if result.Screenshot != "C:\\shots\\1.png" {
		t.Errorf("screenshot=%q", result.Screenshot)
	}
	if transport.playCalls != 2 {
		t.Errorf("playCalls=%d, want 2 (enter and exit Play mode)", transport.playCalls)
	}
	if transport.screenshotCalls != 1 {
		t.Errorf("screenshotCalls=%d, want exactly 1", transport.screenshotCalls)
	}
}

// The failure #36 exists to stop: a script that silently does nothing still
// prints a banner on startup, so the console is non-empty and no marker matches.
// Without evidence that the place was actually running, that is not a pass.
func TestValidateWithoutPlayModeEvidenceIsNotPassLike(t *testing.T) {
	transport := &playtestTransport{
		studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		consoleResponses: []string{"[Output] Server started"},
		editModeOnly:     true,
	}
	p := newProvisioner(t, transport)
	result := p.Validate(context.Background(), fastValidateRequest())
	if result.Outcome != ValidationInconclusive {
		t.Fatalf("outcome=%q, want inconclusive when Play mode was never confirmed", result.Outcome)
	}
	if result.Notice == "" {
		t.Error("an inconclusive outcome must explain itself to the operator")
	}
	if transport.stateCalls == 0 {
		t.Error("expected the loop to ask Studio what mode it was in")
	}
}

// Once Studio has confirmed Play mode there is nothing further to establish, so
// the loop stops asking rather than spending a call per poll.
func TestValidateStopsAskingForStudioStateOncePlayModeIsConfirmed(t *testing.T) {
	transport := &playtestTransport{
		studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		consoleResponses: []string{"[Output] Server started"},
	}
	p := newProvisioner(t, transport)
	req := fastValidateRequest()
	req.Window = 60 * time.Millisecond
	p.Validate(context.Background(), req)
	if transport.consoleCalls < 3 {
		t.Fatalf("consoleCalls=%d, want several polls for this to prove anything", transport.consoleCalls)
	}
	// Instance selection asks for the state once of its own accord, before the
	// window opens; what matters is that the window itself does not ask again
	// per poll once Play mode has been established.
	if transport.stateCalls >= transport.consoleCalls {
		t.Errorf("stateCalls=%d across %d polls, want the question asked once and then dropped", transport.stateCalls, transport.consoleCalls)
	}
}

func TestValidateFailsOnScriptError(t *testing.T) {
	transport := &playtestTransport{
		studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		consoleResponses: []string{"Server started", "ServerScriptService.Main:12: attempt to index nil with 'Humanoid'"},
	}
	p := newProvisioner(t, transport)
	result := p.Validate(context.Background(), fastValidateRequest())
	if result.Outcome != ValidationFailed {
		t.Fatalf("outcome=%q, want failed", result.Outcome)
	}
	if len(result.Errors) == 0 {
		t.Error("expected at least one classified error line")
	}
	if transport.playCalls != 2 {
		t.Errorf("playCalls=%d, want 2 even after a failure", transport.playCalls)
	}
}

// Studio answers every poll with the whole console buffer, so appending each
// poll's text wholesale would repeat everything already seen. The loop must
// shift by the common prefix and append only what grew.
func TestValidateConsoleAppendsOnlyNewTextAcrossPolls(t *testing.T) {
	transport := &playtestTransport{
		studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		consoleResponses: []string{"a", "a\nb", "a\nb\nc"},
	}
	p := newProvisioner(t, transport)
	req := fastValidateRequest()
	req.Window = 60 * time.Millisecond
	result := p.Validate(context.Background(), req)
	for _, line := range []string{"a", "b", "c"} {
		if n := strings.Count(result.Console, line); n != 1 {
			t.Errorf("Console has %q %d times, want exactly once: %q", line, n, result.Console)
		}
	}
}

// A line that genuinely repeats within a single poll's response is real
// output, not an artifact of polling, and must not be collapsed the way
// dedupeEntries collapses repeats across polls.
func TestValidateConsolePreservesLineThatRepeatsWithinOnePoll(t *testing.T) {
	transport := &playtestTransport{
		studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		consoleResponses: []string{"tick\ntick\ntick"},
	}
	p := newProvisioner(t, transport)
	result := p.Validate(context.Background(), fastValidateRequest())
	if n := strings.Count(result.Console, "tick"); n != 3 {
		t.Errorf("Console has %d occurrences of a line repeated within one poll, want 3: %q", n, result.Console)
	}
}

// If a later poll's text does not start with the previous poll's text, the
// buffer rotated or was truncated rather than merely grown. There is no
// common prefix to shift by, so the whole new text must be kept rather than
// discarded as if it were already seen.
func TestValidateConsoleKeepsWholeBufferWhenItDoesNotExtendThePrevious(t *testing.T) {
	transport := &playtestTransport{
		studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		consoleResponses: []string{"a\nb", "x\ny"},
	}
	p := newProvisioner(t, transport)
	req := fastValidateRequest()
	req.Window = 60 * time.Millisecond
	result := p.Validate(context.Background(), req)
	for _, line := range []string{"a", "b", "x", "y"} {
		if !strings.Contains(result.Console, line) {
			t.Errorf("Console lost %q after the buffer rotated: %q", line, result.Console)
		}
	}
}

func TestValidateIsInconclusiveWithNoConsoleSignal(t *testing.T) {
	transport := &playtestTransport{
		studioTransport: studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
	}
	p := newProvisioner(t, transport)
	result := p.Validate(context.Background(), fastValidateRequest())
	if result.Outcome != ValidationInconclusive {
		t.Fatalf("outcome=%q, want inconclusive", result.Outcome)
	}
}

// Studio disappearing partway through (crash, operator closing the window)
// must not be reported as a failed playtest — there is no script signal to
// trust, only a broken connection.
func TestValidateIsInconclusiveWhenStudioClosesMidPlaytest(t *testing.T) {
	transport := &playtestTransport{
		studioTransport: studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		closeAfterCalls: 3, // probe's list+state calls and entering start_stop_play succeed, everything after fails
	}
	p := newProvisioner(t, transport)
	result := p.Validate(context.Background(), fastValidateRequest())
	if result.Outcome != ValidationInconclusive {
		t.Fatalf("outcome=%q notice=%q, want inconclusive", result.Outcome, result.Notice)
	}
	if result.Notice == "" {
		t.Error("an inconclusive result from a broken connection should say why")
	}
}

// A malformed tool response (unparseable JSON) must be tolerated, not crash
// the loop or the daemon.
func TestValidateToleratesMalformedConsoleResponses(t *testing.T) {
	transport := &playtestTransport{
		studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		malformedConsole: true,
	}
	p := newProvisioner(t, transport)
	result := p.Validate(context.Background(), fastValidateRequest())
	if result.Outcome != ValidationInconclusive {
		t.Fatalf("outcome=%q, want inconclusive (no usable console signal)", result.Outcome)
	}
	if transport.playCalls != 2 {
		t.Errorf("playCalls=%d, want 2 (Play mode must still be exited)", transport.playCalls)
	}
}

// Validate must reuse the same single-instance / target-matching rule as
// Provision: no Studio, or more than one candidate, must not attempt to enter
// Play mode on an ambiguous or absent instance.
func TestValidateSkipsWhenNoSingleStudioInstance(t *testing.T) {
	transport := &playtestTransport{studioTransport: studioTransport{instances: nil}}
	p := newProvisioner(t, transport)
	result := p.Validate(context.Background(), fastValidateRequest())
	if result.Outcome != ValidationInconclusive {
		t.Fatalf("outcome=%q, want inconclusive", result.Outcome)
	}
	if transport.playCalls != 0 {
		t.Errorf("playCalls=%d, want 0 when no single instance is available", transport.playCalls)
	}
}

func TestValidateSkipsWhenLauncherIsAbsent(t *testing.T) {
	p := newProvisioner(t, &playtestTransport{})
	p.Override = func() string { return "" }
	// Force DetectLauncher down the "no override, no LOCALAPPDATA launcher"
	// path by pointing somewhere nonexistent for whatever OS runs this test.
	dir := t.TempDir()
	p.Override = func() string { return dir + "/definitely-missing-launcher" }
	result := p.Validate(context.Background(), fastValidateRequest())
	if result.Outcome != ValidationInconclusive {
		t.Fatalf("outcome=%q, want inconclusive", result.Outcome)
	}
}
