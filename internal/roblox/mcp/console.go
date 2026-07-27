package mcp

import (
	"regexp"
	"strconv"
	"strings"
)

// Severity is how Studio itself classified one line of output, as opposed to
// how alarming its wording happens to look.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// ConsoleEntry is one line of Studio console output taken apart: what Studio
// said, how it graded it, and — when the line carried them — which script and
// line it came from.
type ConsoleEntry struct {
	Severity Severity
	Message  string
	Script   string
	Line     int
	Stack    []string
	// Raw is the line as it arrived, so anything shown to an operator can fall
	// back to exactly what Studio printed rather than this package's reading of
	// it.
	Raw string
}

// Failure reports whether this entry is the kind of thing a playtest should
// fail on. Severity decides it, with one exception: Roblox prints an infinite
// yield as a warning, and a script stuck forever on a WaitForChild is a broken
// game whatever Studio grades it.
func (e ConsoleEntry) Failure() bool {
	if e.Severity == SeverityError {
		return true
	}
	return e.Severity == SeverityWarning && strings.Contains(strings.ToLower(e.Message), "infinite yield")
}

var (
	// A leading wall-clock stamp, with or without Studio's " -- " separator.
	consoleTimestamp = regexp.MustCompile(`^\d{1,2}:\d{2}:\d{2}(\.\d+)?\s*(--\s*)?`)
	// A bracketed or colon-terminated grade at the head of the line.
	consoleSeverityTag = regexp.MustCompile(`(?i)^\[(error|warn|warning|info|output)\]\s*|^(error|warning|info)\s*:\s*`)
	// Studio's own MessageType names, as the enum rather than as prose.
	consoleMessageType = regexp.MustCompile(`(?i)^message(error|warning|info|output)\s*[:\-]?\s*`)
	// Roblox's script attribution: a dotted instance path, a line number, and a
	// colon before the message itself.
	consoleScriptRef = regexp.MustCompile(`^([A-Za-z_][\w]*(?:\.[\w ]+)*):(\d+):\s*`)
)

// ParseConsole takes apart raw console text into entries and reports whether
// the text was structured enough to be worth classifying on.
//
// The second return is deliberately not "no errors were found": it means the
// parse itself landed, so a caller can tell "Studio told us it was fine" from
// "we could not read what Studio said" and fall back rather than guess. Text
// carrying no severity grade and no script attribution anywhere parses as
// false, whatever it contains.
func ParseConsole(text string) ([]ConsoleEntry, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	entries := make([]ConsoleEntry, 0, len(lines))
	structured := false
	inStack := false

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		body := consoleTimestamp.ReplaceAllString(line, "")

		// A stack trace belongs to the entry above it rather than standing on
		// its own, so the run view shows one error with its trace instead of
		// five unattributed lines.
		if lower := strings.ToLower(body); strings.HasPrefix(lower, "stack begin") {
			inStack = true
			structured = true
			continue
		} else if strings.HasPrefix(lower, "stack end") {
			inStack = false
			continue
		}
		if inStack {
			if len(entries) > 0 {
				entries[len(entries)-1].Stack = append(entries[len(entries)-1].Stack, body)
			}
			continue
		}

		entry := ConsoleEntry{Severity: SeverityInfo, Raw: raw}
		graded := false
		if tag := consoleMessageType.FindStringSubmatch(body); tag != nil {
			entry.Severity = severityFor(tag[1])
			body = consoleMessageType.ReplaceAllString(body, "")
			graded = true
		} else if tag := consoleSeverityTag.FindStringSubmatch(body); tag != nil {
			entry.Severity = severityFor(tag[1] + tag[2])
			body = consoleSeverityTag.ReplaceAllString(body, "")
			graded = true
		}

		if ref := consoleScriptRef.FindStringSubmatch(body); ref != nil {
			entry.Script = ref[1]
			if n, err := strconv.Atoi(ref[2]); err == nil {
				entry.Line = n
			}
			body = consoleScriptRef.ReplaceAllString(body, "")
			structured = true
			// A bare script reference with no grade is how Roblox prints an
			// unhandled runtime error, which is the single most important thing
			// this whole loop is looking for.
			if !graded {
				entry.Severity = SeverityError
			}
		}
		if graded {
			structured = true
		}

		entry.Message = strings.TrimSpace(body)
		if entry.Message == "" {
			continue
		}
		entries = append(entries, entry)
	}

	return dedupeEntries(entries), structured
}

func severityFor(tag string) Severity {
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case "error":
		return SeverityError
	case "warn", "warning":
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

// dedupeEntries collapses entries the loop saw more than once.
//
// The console is polled repeatedly within one window and Studio answers each
// poll with the whole buffer, not with what is new since the last one, so every
// line arrives once per remaining tick. Without this a single error is reported
// ten times over and the operator is handed a wall of duplicates.
func dedupeEntries(entries []ConsoleEntry) []ConsoleEntry {
	seen := make(map[string]bool, len(entries))
	out := make([]ConsoleEntry, 0, len(entries))
	for _, entry := range entries {
		key := string(entry.Severity) + "\x00" + entry.Script + "\x00" + strconv.Itoa(entry.Line) + "\x00" + entry.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, entry)
	}
	return out
}

// Format renders one entry for an operator: script and line when the line
// carried them, and the message on its own when it did not.
func (e ConsoleEntry) Format() string {
	if e.Script == "" {
		return e.Message
	}
	if e.Line > 0 {
		return e.Script + ":" + strconv.Itoa(e.Line) + " — " + e.Message
	}
	return e.Script + " — " + e.Message
}
