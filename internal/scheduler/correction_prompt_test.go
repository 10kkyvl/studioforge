package scheduler

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- correctionPrompt ---

// TestCorrectionPromptStatesTheConfiguredWindowNotAHardcodedOne is the key
// regression guard for the rewritten correctionPrompt: it must report the
// playtest window that was actually configured for this run, read from
// ValidationResult.Window, rather than a value baked into the prompt text.
func TestCorrectionPromptStatesTheConfiguredWindowNotAHardcodedOne(t *testing.T) {
	thirty := correctionPrompt(ValidationResult{Window: 30 * time.Second})
	if !strings.Contains(thirty, "30 seconds") {
		t.Errorf("a 30s window must be reported as 30 seconds: %q", thirty)
	}

	fortyFive := correctionPrompt(ValidationResult{Window: 45 * time.Second})
	if !strings.Contains(fortyFive, "45 seconds") {
		t.Errorf("a 45s window must be reported as 45 seconds: %q", fortyFive)
	}
	if strings.Contains(fortyFive, "30 seconds") {
		t.Errorf("a 45s window must not still say 30 seconds: %q", fortyFive)
	}
}

func TestCorrectionPromptStatesNoPlayerInputWasSent(t *testing.T) {
	prompt := correctionPrompt(ValidationResult{})
	if !strings.Contains(prompt, "No player input was sent") {
		t.Errorf("prompt must state that no player input was sent: %q", prompt)
	}
}

func TestCorrectionPromptWithUnknownWindowDoesNotClaimADuration(t *testing.T) {
	prompt := correctionPrompt(ValidationResult{Window: 0})
	if prompt == "" {
		t.Fatal("prompt must still render for an unknown (zero) window")
	}
	if strings.Contains(prompt, " for 0 seconds") {
		t.Errorf("a zero window must not be reported as a duration: %q", prompt)
	}
}

// TestCorrectionPromptOrdersCauseFixVerify asserts the three work-order
// instructions appear as cause, then fix, then verify.
func TestCorrectionPromptOrdersCauseFixVerify(t *testing.T) {
	prompt := correctionPrompt(ValidationResult{})
	cause := strings.Index(prompt, "Identify which of the changes you just made causes this")
	fix := strings.Index(prompt, "Fix the cause, not the symptom")
	verify := strings.Index(prompt, "Confirm the fix against the same signal that caught it")
	if cause < 0 || fix < 0 || verify < 0 {
		t.Fatalf("expected all three instructions present: cause=%d fix=%d verify=%d in %q", cause, fix, verify, prompt)
	}
	if !(cause < fix && fix < verify) {
		t.Errorf("instructions out of order: cause=%d fix=%d verify=%d", cause, fix, verify)
	}
}

func TestCorrectionPromptStatesTheCorrectionIsItselfPlaytested(t *testing.T) {
	prompt := correctionPrompt(ValidationResult{})
	if !strings.Contains(prompt, "This correction is playtested the same way when you finish") {
		t.Errorf("prompt must state the correction is itself playtested: %q", prompt)
	}
}

// TestCorrectionPromptGivesASanctionedFalsePositivePath covers the escape
// hatch for the classifier's known failure mode: a plain substring match
// over console text, with no understanding of what it matched.
func TestCorrectionPromptGivesASanctionedFalsePositivePath(t *testing.T) {
	prompt := correctionPrompt(ValidationResult{})
	if !strings.Contains(prompt, "classified by substring matching") {
		t.Errorf("prompt must explain the classifier is a substring match: %q", prompt)
	}
	if !strings.Contains(prompt, "change nothing") || !strings.Contains(prompt, "valid outcome") {
		t.Errorf("prompt must sanction reporting a false positive and changing nothing: %q", prompt)
	}
}

// countBulletLines counts the "- " prefixed error-line bullets a correction
// prompt renders. Nothing else in the prompt template starts a line that way.
func countBulletLines(prompt string) int {
	count := 0
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "- ") {
			count++
		}
	}
	return count
}

// syntheticErrorLines builds n distinct, equal-length fixture error lines.
// Equal length and zero-padded indices matter: it guarantees no line is ever
// an accidental substring of another, so a "line N is present/absent" check
// can never pass or fail for the wrong reason.
func syntheticErrorLines(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("synthetic error %03d", i)
	}
	return lines
}

func TestCorrectionPromptBoundsErrorLinesAndStatesTheTotalCount(t *testing.T) {
	lines := syntheticErrorLines(40)
	prompt := correctionPrompt(ValidationResult{Errors: lines})

	if got := countBulletLines(prompt); got != maxCorrectionErrorLines {
		t.Errorf("bullet lines=%d, want %d (bounded by maxCorrectionErrorLines)", got, maxCorrectionErrorLines)
	}
	if !strings.Contains(prompt, "40 found") {
		t.Errorf("prompt must still state the true total of 40: %q", prompt)
	}
	if !strings.Contains(prompt, lines[0]) {
		t.Errorf("the first error line must be present: %q", prompt)
	}
	if strings.Contains(prompt, lines[39]) {
		t.Errorf("the last error line must have been truncated away: %q", prompt)
	}
}

func TestCorrectionPromptAtExactlyTheCapShowsAllLinesWithNoTruncationNotice(t *testing.T) {
	lines := syntheticErrorLines(maxCorrectionErrorLines)
	prompt := correctionPrompt(ValidationResult{Errors: lines})

	if strings.Contains(prompt, "shown)") {
		t.Errorf("exactly-at-the-cap must not print a truncation notice: %q", prompt)
	}
	if !strings.Contains(prompt, "Console lines classified as errors:\n") {
		t.Errorf("header must carry no parenthetical when nothing was truncated: %q", prompt)
	}
	for _, line := range lines {
		if !strings.Contains(prompt, line) {
			t.Errorf("line %q missing from prompt: %q", line, prompt)
		}
	}
	if got := countBulletLines(prompt); got != maxCorrectionErrorLines {
		t.Errorf("bullet lines=%d, want %d", got, maxCorrectionErrorLines)
	}
}

// TestCorrectionPromptWithNoErrorsOmitsTheErrorSectionAndIsNeverEmpty adds an
// independent, direct check alongside the existing coverage in
// validation_test.go (TestValidationFailureSchedulesACorrectionRun /
// TestValidationFailureExhaustedProposesADecisionWhenAProposerIsInstalled),
// which already assert a scheduled correction's prompt is never empty.
func TestCorrectionPromptWithNoErrorsOmitsTheErrorSectionAndIsNeverEmpty(t *testing.T) {
	prompt := correctionPrompt(ValidationResult{})
	if prompt == "" {
		t.Fatal("a correction prompt must never be empty")
	}
	if strings.Contains(prompt, "Console lines classified as errors") {
		t.Errorf("no error section header should appear when there are no errors: %q", prompt)
	}
}

func TestCorrectionPromptScreenshotPathPresentOnlyWhenSet(t *testing.T) {
	withShot := correctionPrompt(ValidationResult{Screenshot: "C:\\shots\\1.png"})
	if !strings.Contains(withShot, "A screenshot was captured during the playtest:") {
		t.Errorf("prompt must mention the screenshot when one was captured: %q", withShot)
	}
	if !strings.Contains(withShot, "C:\\shots\\1.png") {
		t.Errorf("prompt must include the screenshot path: %q", withShot)
	}

	without := correctionPrompt(ValidationResult{})
	if strings.Contains(without, "A screenshot was captured during the playtest") {
		t.Errorf("prompt must not mention a screenshot when none was captured: %q", without)
	}
}

// --- formatWindow ---

// The window is an operator setting, so a correction prompt can end up stating
// any of these. It reads as prose in the middle of a sentence, which is why it
// exists at all rather than printing Go's own "1m30s".
func TestFormatWindow(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"thirty seconds", 30 * time.Second, "30 seconds"},
		{"forty-five seconds", 45 * time.Second, "45 seconds"},
		{"exactly one minute", 60 * time.Second, "1 minute"},
		{"exactly two minutes", 120 * time.Second, "2 minutes"},
		{"a minute and a half", 90 * time.Second, "1 minute 30 seconds"},
		{"two and a half minutes", 150 * time.Second, "2 minutes 30 seconds"},
		{"one second", 1 * time.Second, "1 seconds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatWindow(tc.d); got != tc.want {
				t.Errorf("formatWindow(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}

// --- withStudioRules ---

func TestWithStudioRulesZeroGrantLeavesPromptUnchanged(t *testing.T) {
	prompt := "You are an agent working on a Roblox place.\n\nDo the assigned task."
	got := withStudioRules(prompt, MCPGrant{})
	if got != prompt {
		t.Errorf("a zero grant must leave the prompt unchanged:\ngot:  %q\nwant: %q", got, prompt)
	}
}

func TestWithStudioRulesNoticeWithoutGrantAppendsWithheldLineNamingNoTool(t *testing.T) {
	base := "System prompt body."
	notice := "Studio was not running when this run started"
	got := withStudioRules(base, MCPGrant{Notice: notice})

	if !strings.HasPrefix(got, base+"\n\n") {
		t.Fatalf("withheld notice must be appended after the base prompt: %q", got)
	}
	if !strings.Contains(got, notice) {
		t.Errorf("withheld section must include the notice text %q: %q", notice, got)
	}
	for _, tool := range []string{"script_read", "generate_mesh", "insert_asset", "start_stop_play", "screen_capture", "get_console_output"} {
		if strings.Contains(got, tool) {
			t.Errorf("a withheld grant must not name Studio tool %q: %q", tool, got)
		}
	}
}

// studioReadOnlyToolsPrefixed mirrors what mcp.AllowedTools("read-only") in
// internal/roblox/mcp hands a read-only run (see readOnlyTools and ToolPrefix
// in internal/roblox/mcp/config.go). The scheduler package deliberately stays
// provider-neutral and does not import internal/roblox/mcp (see the
// ValidationOutcome doc comment in scheduler.go), so the exact prefixed
// tool-name strings are reproduced here as literals instead of imported.
var studioReadOnlyToolsPrefixed = []string{
	"mcp__Roblox_Studio__script_read",
	"mcp__Roblox_Studio__script_search",
	"mcp__Roblox_Studio__script_grep",
	"mcp__Roblox_Studio__search_game_tree",
	"mcp__Roblox_Studio__inspect_instance",
	"mcp__Roblox_Studio__get_studio_state",
	"mcp__Roblox_Studio__get_console_output",
	"mcp__Roblox_Studio__screen_capture",
	"mcp__Roblox_Studio__list_roblox_studios",
	"mcp__Roblox_Studio__set_active_studio",
}

func TestWithStudioRulesGrantedReadOnlyNamesScreenCaptureNotWorkspaceTools(t *testing.T) {
	grant := MCPGrant{ConfigPath: "x.json", AllowedTools: studioReadOnlyToolsPrefixed}
	got := withStudioRules("Base prompt.", grant)

	if !strings.Contains(got, "screen_capture") {
		t.Errorf("a granted read-only section must name screen_capture: %q", got)
	}
	for _, tool := range []string{"generate_mesh", "insert_asset", "start_stop_play"} {
		if strings.Contains(got, tool) {
			t.Errorf("a read-only grant must not name workspace tool %q: %q", tool, got)
		}
	}
}

func TestWithStudioRulesEmptyBasePromptReturnsJustTheSection(t *testing.T) {
	grant := MCPGrant{ConfigPath: "x.json", AllowedTools: studioReadOnlyToolsPrefixed}
	withBase := withStudioRules("Base prompt.", grant)
	wantSection := strings.TrimPrefix(withBase, "Base prompt.\n\n")
	if wantSection == withBase {
		t.Fatal("test setup broken: a non-empty base prompt did not get the expected separator")
	}

	got := withStudioRules("", grant)
	if got != wantSection {
		t.Errorf("an empty base prompt must return just the section:\ngot:  %q\nwant: %q", got, wantSection)
	}
	if strings.HasPrefix(got, "\n") {
		t.Errorf("must have no leading blank lines: %q", got)
	}
}
