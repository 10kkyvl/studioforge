package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestRealStudioConsoleFormat settles what internal/roblox/mcp/console.go has to
// parse, against a Studio that is actually open rather than against the mocks,
// which reflect what the adapter expects rather than every shape Studio emits.
//
// It reads the console as it stands and reports what ParseConsole made of it. It
// deliberately prints nothing into the console first: a probe that writes its
// own lines measures its own formatting, not Studio's, and an operator's run may
// be using the same console.
//
//	STUDIOFORGE_REAL_STUDIO=1
func TestRealStudioConsoleFormat(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_STUDIO") != "1" {
		t.Skip("set STUDIOFORGE_REAL_STUDIO=1 with Roblox Studio open to run the live smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := connectToOneStudio(ctx, t)

	// Read twice back to back. Whether the second read repeats the first decides
	// whether the poll loop accumulates duplicates (issue #38) or whether the
	// tool drains what it returns — and the two call for opposite handling.
	var reads []string
	for i := 0; i < 2; i++ {
		raw, err := client.Call(ctx, "get_console_output", nil)
		if err != nil {
			t.Fatalf("get_console_output #%d: %v", i+1, err)
		}
		got, err := TextResult(raw)
		if err != nil {
			t.Fatalf("read console #%d: %v", i+1, err)
		}
		reads = append(reads, got)
	}
	text := reads[0]
	t.Logf("raw console output (%d bytes):\n%s", len(text), text)
	switch {
	case reads[0] == "" && reads[1] == "":
		t.Log("buffer semantics: undetermined — the console was empty both times")
	case reads[1] == reads[0]:
		t.Logf("buffer semantics: REPEATS (second read returned the same %d bytes) — the poll loop accumulates duplicates", len(reads[1]))
	case reads[1] == "":
		t.Log("buffer semantics: DRAINS (second read returned nothing) — each poll returns only what is new")
	default:
		t.Logf("buffer semantics: second read differed (%d bytes):\n%s", len(reads[1]), reads[1])
	}

	entries, structured := ParseConsole(text)
	t.Logf("structured=%v entries=%d", structured, len(entries))
	for i, entry := range entries {
		if i >= 25 {
			t.Logf("... %d more", len(entries)-i)
			break
		}
		t.Logf("  [%s] script=%q line=%d stack=%d msg=%q", entry.Severity, entry.Script, entry.Line, len(entry.Stack), entry.Message)
	}
	outcome, errs, _, by := classifyConsole(text)
	t.Logf("classified as %q by %q with %d failing lines", outcome, by, len(errs))
	for _, line := range errs {
		t.Logf("  failure: %s", line)
	}
	if strings.TrimSpace(text) == "" {
		t.Skip("the console was empty, so this run establishes nothing about its format")
	}
}

// connectToOneStudio dials the launcher and selects the single open Studio,
// which every live probe in this package needs before it can ask anything.
func connectToOneStudio(ctx context.Context, t *testing.T) *Client {
	t.Helper()
	launch, err := DetectLauncher("")
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewStdioTransport(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(transport)
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Discover(ctx); err != nil {
		t.Fatal(err)
	}
	var instances []Instance
	deadline := time.Now().Add(15 * time.Second)
	for {
		instances, err = client.ListStudios(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(instances) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if len(instances) != 1 {
		t.Skipf("want exactly one open Studio, got %d", len(instances))
	}
	if err := client.SelectStudio(ctx, instances[0].ID); err != nil {
		t.Fatal(err)
	}
	return client
}
