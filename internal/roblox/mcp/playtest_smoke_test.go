package mcp

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/attachments"
	"github.com/10kkyvl/studioforge/internal/roblox/studio"
)

// TestRealStudioPlaytest runs the actual validation pass against whatever Studio
// is open: enter Play mode, poll the console, capture at the end of the window,
// leave Play mode. It is the only thing that can show the capture timing and the
// image storage working against a real Studio rather than against a fake that
// answers the way we expect.
//
// It drives the operator's own Studio into Play mode for the length of the
// window, so it is opt-in twice over.
//
//	STUDIOFORGE_REAL_STUDIO=1 STUDIOFORGE_REAL_PLAYTEST=1
func TestRealStudioPlaytest(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_STUDIO") != "1" || os.Getenv("STUDIOFORGE_REAL_PLAYTEST") != "1" {
		t.Skip("set STUDIOFORGE_REAL_STUDIO=1 and STUDIOFORGE_REAL_PLAYTEST=1 with Roblox Studio open; this enters Play mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	project := t.TempDir()
	// Running is what separates "no Studio is open" from "a Studio is open but
	// has not attached to this launcher yet"; without it the attach wait gives
	// up on the first empty listing.
	p := &Provisioner{Dir: t.TempDir(), Running: studio.IsRunning}
	started := time.Now()
	result := p.Validate(ctx, ValidateRequest{
		Window:       15 * time.Second,
		PollInterval: 3 * time.Second,
		ProjectPath:  project,
	})
	elapsed := time.Since(started)

	t.Logf("outcome=%s window=%s elapsed=%s", result.Outcome, result.Window, elapsed.Round(time.Second))
	if result.Notice != "" {
		t.Logf("notice: %s", result.Notice)
	}
	if len(result.Errors) > 0 {
		t.Logf("classified as errors (%d):", len(result.Errors))
		for _, line := range result.Errors {
			t.Logf("  - %s", line)
		}
	}
	if console := strings.TrimSpace(result.Console); console != "" {
		t.Logf("console (%d bytes, first 400): %.400s", len(console), console)
	} else {
		t.Log("console: empty")
	}

	if result.Outcome == ValidationInconclusive && result.Screenshot == "" {
		t.Skipf("the playtest produced no signal at all: %s", result.Notice)
	}

	// The window it reports must be the window it was asked for, which is what
	// the correction prompt now states to the agent.
	if result.Window != 15*time.Second {
		t.Errorf("Window=%s, want the 15s that was requested", result.Window)
	}
	// It must have actually held Play mode for roughly the window, rather than
	// returning early — that is what "captured at the end" depends on.
	if elapsed < 15*time.Second {
		t.Errorf("the pass took %s, less than its own window; Play mode was not held", elapsed.Round(time.Second))
	}

	if result.Screenshot == "" {
		t.Fatal("no screenshot was captured")
	}
	t.Logf("screenshot=%s", result.Screenshot)
	if !strings.HasPrefix(result.Screenshot, attachments.Dir+"/") {
		t.Fatalf("screenshot=%q is a bare reference, not a stored image — the image block was not decoded", result.Screenshot)
	}
	if !attachments.ValidRef(project, result.Screenshot) {
		t.Fatalf("screenshot=%q does not resolve inside the project", result.Screenshot)
	}
	body, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(result.Screenshot)))
	if err != nil {
		t.Fatal(err)
	}
	sniffed := http.DetectContentType(body)
	t.Logf("stored %d bytes, sniffed as %s", len(body), sniffed)
	if !strings.HasPrefix(sniffed, "image/") {
		t.Fatalf("the stored file is %s, not an image", sniffed)
	}
	// A capture of a real Studio viewport is not a handful of bytes; a tiny file
	// would mean something answered with a placeholder.
	if len(body) < 4096 {
		t.Errorf("the stored image is only %d bytes — suspiciously small for a viewport capture", len(body))
	}
	// Keep it where the operator can look at it, since the point of the change
	// is that a human can now see what the playtest saw.
	if keep := os.Getenv("STUDIOFORGE_KEEP_SCREENSHOT"); keep != "" {
		if err := os.WriteFile(keep, body, 0o600); err != nil {
			t.Logf("could not copy the screenshot to %s: %v", keep, err)
		} else {
			t.Logf("screenshot copied to %s", keep)
		}
	}
}
