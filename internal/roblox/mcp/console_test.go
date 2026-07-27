package mcp

import "testing"

func TestParseConsoleReadsRobloxErrorAttribution(t *testing.T) {
	entries, structured := ParseConsole("ServerScriptService.Main:12: attempt to index nil with 'Humanoid'")
	if !structured {
		t.Fatal("a line carrying a script and a line number is structured output")
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%+v, want one", entries)
	}
	got := entries[0]
	if got.Script != "ServerScriptService.Main" || got.Line != 12 {
		t.Errorf("script=%q line=%d", got.Script, got.Line)
	}
	if got.Severity != SeverityError {
		t.Errorf("severity=%q, want error — an attributed runtime failure is an error however it is worded", got.Severity)
	}
	if got.Message != "attempt to index nil with 'Humanoid'" {
		t.Errorf("message=%q", got.Message)
	}
}

func TestParseConsoleStripsTimestamps(t *testing.T) {
	entries, _ := ParseConsole("14:32:16.456  --  [Error] Workspace.Part.Script:5: bad thing")
	if len(entries) != 1 {
		t.Fatalf("entries=%+v, want one", entries)
	}
	if entries[0].Script != "Workspace.Part.Script" || entries[0].Line != 5 {
		t.Errorf("entry=%+v", entries[0])
	}
	if entries[0].Message != "bad thing" {
		t.Errorf("message=%q, want the timestamp and grade stripped", entries[0].Message)
	}
}

func TestParseConsoleReadsSeverityGrades(t *testing.T) {
	for _, tc := range []struct {
		line string
		want Severity
	}{
		{"[Error] something broke", SeverityError},
		{"[Warning] something is odd", SeverityWarning},
		{"[Warn] something is odd", SeverityWarning},
		{"[Info] something happened", SeverityInfo},
		{"[Output] something printed", SeverityInfo},
		{"Error: something broke", SeverityError},
		{"MessageError something broke", SeverityError},
		{"MessageWarning: something is odd", SeverityWarning},
	} {
		entries, structured := ParseConsole(tc.line)
		if !structured {
			t.Errorf("%q did not parse as structured", tc.line)
			continue
		}
		if len(entries) != 1 || entries[0].Severity != tc.want {
			t.Errorf("%q -> %+v, want severity %q", tc.line, entries, tc.want)
		}
	}
}

func TestParseConsoleAttachesStackTraceToItsError(t *testing.T) {
	entries, _ := ParseConsole("ServerScriptService.Main:12: attempt to call a nil value\nStack Begin\nScript 'ServerScriptService.Main', Line 12\nScript 'ServerScriptService.Main', Line 4\nStack End\n[Output] carry on")
	if len(entries) != 2 {
		t.Fatalf("entries=%+v, want the error and the later output, with the trace folded in", entries)
	}
	if len(entries[0].Stack) != 2 {
		t.Errorf("stack=%v, want both frames on the error", entries[0].Stack)
	}
	if entries[1].Message != "carry on" {
		t.Errorf("the line after Stack End belongs to its own entry, got %+v", entries[1])
	}
}

// Ordinary output with no grade and no attribution is exactly the case the
// phrase-matching fallback still exists for.
func TestParseConsoleReportsUnstructuredOutputAsSuch(t *testing.T) {
	entries, structured := ParseConsole("Server started\nPlayer joined the game")
	if structured {
		t.Error("plain prose carries neither a grade nor an attribution and must not claim to parse")
	}
	if len(entries) != 2 {
		t.Errorf("entries=%+v, want the lines kept even when unstructured", entries)
	}
}

func TestParseConsoleHandlesEmptyAndWhitespaceInput(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\n\n", "\r\n\r\n"} {
		entries, structured := ParseConsole(in)
		if structured || len(entries) != 0 {
			t.Errorf("%q -> entries=%+v structured=%v, want nothing", in, entries, structured)
		}
	}
}

// A window that ends mid-line, a stack that never closes, and a line number too
// large to be a line number all have to resolve to something rather than panic.
func TestParseConsoleToleratesMalformedAndTruncatedInput(t *testing.T) {
	for _, in := range []string{
		"ServerScriptService.Main:12: attempt to inde",
		"Stack Begin\nScript 'A', Line 1",
		"Stack End\norphaned frame",
		"Main:99999999999999999999: overflowing line number",
		":::::",
		"[Error]",
		"14:32:16.456 --",
	} {
		entries, _ := ParseConsole(in)
		for _, entry := range entries {
			if entry.Message == "" {
				t.Errorf("%q produced an entry with no message: %+v", in, entry)
			}
		}
	}
}

func TestParseConsoleDedupesRepeatedLines(t *testing.T) {
	one := "[Error] ServerScriptService.Main:12: broke\n[Output] fine\n"
	entries, _ := ParseConsole(one + one + one + one)
	if len(entries) != 2 {
		t.Fatalf("entries=%+v, want each distinct line once", entries)
	}
}

func TestConsoleEntryFailureFollowsSeverityWithOneException(t *testing.T) {
	if !(ConsoleEntry{Severity: SeverityError, Message: "anything"}).Failure() {
		t.Error("an error is a failure")
	}
	if (ConsoleEntry{Severity: SeverityWarning, Message: "deprecated API"}).Failure() {
		t.Error("an ordinary warning is not a failure")
	}
	if !(ConsoleEntry{Severity: SeverityWarning, Message: "Infinite yield possible on 'X:WaitForChild(\"Y\")'"}).Failure() {
		t.Error("Roblox grades an infinite yield as a warning, but a script stuck forever is a broken game")
	}
	if (ConsoleEntry{Severity: SeverityInfo, Message: "loaded the error handler"}).Failure() {
		t.Error("ordinary output must not fail on its wording")
	}
}

func TestConsoleEntryFormat(t *testing.T) {
	if got := (ConsoleEntry{Script: "A.B", Line: 7, Message: "boom"}).Format(); got != "A.B:7 — boom" {
		t.Errorf("format=%q", got)
	}
	if got := (ConsoleEntry{Message: "boom"}).Format(); got != "boom" {
		t.Errorf("format=%q", got)
	}
	if got := (ConsoleEntry{Script: "A.B", Message: "boom"}).Format(); got != "A.B — boom" {
		t.Errorf("format=%q", got)
	}
}
