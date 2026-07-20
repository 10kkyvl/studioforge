package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestClassifyConsoleWithNoOutputIsInconclusive(t *testing.T) {
	outcome, errs := classifyConsole("")
	if outcome != ValidationInconclusive {
		t.Fatalf("outcome=%q, want inconclusive", outcome)
	}
	if len(errs) != 0 {
		t.Errorf("errs=%v, want none", errs)
	}
}

func TestClassifyConsoleWithOnlyOrdinaryOutputPasses(t *testing.T) {
	outcome, errs := classifyConsole("Server started\nPlayer joined the game\nRound 1 begins")
	if outcome != ValidationPassed {
		t.Fatalf("outcome=%q, want passed", outcome)
	}
	if len(errs) != 0 {
		t.Errorf("errs=%v, want none", errs)
	}
}

func TestClassifyConsoleDetectsAScriptError(t *testing.T) {
	outcome, errs := classifyConsole("Server started\nServerScriptService.Main:12: attempt to index nil with 'Humanoid'\nRound 1 begins")
	if outcome != ValidationFailed {
		t.Fatalf("outcome=%q, want failed", outcome)
	}
	if !reflect.DeepEqual(errs, []string{"ServerScriptService.Main:12: attempt to index nil with 'Humanoid'"}) {
		t.Errorf("errs=%v", errs)
	}
}

func TestClassifyConsoleDetectsInfiniteYield(t *testing.T) {
	outcome, errs := classifyConsole("Infinite yield possible on 'ReplicatedStorage:WaitForChild(\"Remote\")'")
	if outcome != ValidationFailed {
		t.Fatalf("outcome=%q, want failed", outcome)
	}
	if len(errs) != 1 {
		t.Fatalf("errs=%v, want one line", errs)
	}
}

func TestClassifyConsoleCollectsMultipleErrorLines(t *testing.T) {
	outcome, errs := classifyConsole("attempt to call a nil value\nfine line\nX is not a valid member of Instance")
	if outcome != ValidationFailed {
		t.Fatalf("outcome=%q, want failed", outcome)
	}
	if len(errs) != 2 {
		t.Fatalf("errs=%v, want two lines", errs)
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

	totalCalls      int
	playCalls       int
	consoleCalls    int
	screenshotCalls int
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

func TestValidatePassesWhenConsoleIsClean(t *testing.T) {
	transport := &playtestTransport{
		studioTransport:  studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		consoleResponses: []string{"Server started", "Player joined"},
		screenshotText:   "C:\\shots\\1.png",
	}
	p := newProvisioner(t, transport)
	result := p.Validate(context.Background(), fastValidateRequest())
	if result.Outcome != ValidationPassed {
		t.Fatalf("outcome=%q notice=%q, want passed", result.Outcome, result.Notice)
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
