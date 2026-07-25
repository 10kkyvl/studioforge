package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/roblox/studio"
)

// TestRealStudioGrantsAccessForTheConfiguredPlace runs the actual access gate
// against whatever Studio is open, which is the only way to check the one thing
// unit tests cannot: that the name this project matches on is the name the
// launcher really reports. A place opened from roblox.com reports its display
// name and no file name at all, so a project edited that way is refused unless
// STUDIOFORGE_REAL_CLOUD_PLACE names it.
//
//	STUDIOFORGE_REAL_STUDIO=1
//	STUDIOFORGE_REAL_PLACE_FILE=my-game-a1b2c3d4.rbxl
//	STUDIOFORGE_REAL_CLOUD_PLACE="My Game"   # optional, for a roblox.com place
func TestRealStudioGrantsAccessForTheConfiguredPlace(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_STUDIO") != "1" {
		t.Skip("set STUDIOFORGE_REAL_STUDIO=1 with Roblox Studio open to run the live smoke")
	}
	place := Place{
		FileName:  os.Getenv("STUDIOFORGE_REAL_PLACE_FILE"),
		CloudName: os.Getenv("STUDIOFORGE_REAL_CLOUD_PLACE"),
	}
	if !place.Named() {
		t.Skip("set STUDIOFORGE_REAL_PLACE_FILE and/or STUDIOFORGE_REAL_CLOUD_PLACE to name the place to match")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// Running is what separates "no Studio is open" from "a Studio is open but
	// another MCP client holds its connection" — the launcher reports both as an
	// empty list, and this smoke runs alongside a StudioForge daemon that
	// competes for exactly that slot, so without it a refusal names the wrong
	// cause.
	p := &Provisioner{Dir: t.TempDir(), Running: studio.IsRunning}
	// Open is deliberately nil: this checks recognition of an already-open
	// Studio, and must never build or launch anything of its own.
	grant := p.Provision(ctx, "live-smoke", "workspace-write", Target{Place: place})
	if grant.Release != nil {
		t.Cleanup(grant.Release)
	}
	if grant.ConfigPath == "" {
		t.Fatalf("the open Studio was refused for %s: %s", place, grant.Notice)
	}
	t.Logf("granted for %s: tools=%d state=%q", place, len(grant.AllowedTools), grant.Context)
}

func TestRealStudioMCP(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_STUDIO") != "1" {
		t.Skip("set STUDIOFORGE_REAL_STUDIO=1 with Roblox Studio open to run the live smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	launch, err := DetectLauncher("")
	if err != nil {
		t.Fatal(err)
	}
	launch.Args = append(launch.Args, "--verbose")
	transport, err := NewStdioTransport(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(transport)
	t.Cleanup(func() { _ = client.Close() })
	// The advertised tool list is recorded but never gated on: a launcher that
	// did not win the WS host port is pushed zero tools for its whole life while
	// its calls still succeed. Skipping here is what hid the defect that made
	// StudioForge report no Studio while one was open.
	tools, err := client.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("advertised tools=%d (zero is expected when another MCP client holds the host)", len(tools))
	type studioListing struct {
		Studios []struct {
			ID string `json:"id"`
		} `json:"studios"`
	}
	var listing studioListing
	deadline := time.NewTimer(10 * time.Second)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for len(listing.Studios) == 0 {
		result, err := client.Call(ctx, "list_roblox_studios", nil)
		if err != nil {
			t.Fatal(err)
		}
		text, err := TextResult(result)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(text), &listing); err != nil {
			t.Fatalf("decode live Studio list %q: %v", text, err)
		}
		if len(listing.Studios) > 0 {
			break
		}
		select {
		case <-deadline.C:
			t.Skip("Studio MCP is connected but reported no Studio instances after 10 seconds")
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if len(listing.Studios) == 0 {
		t.Skip("Studio MCP is connected but reported no Studio instances")
	}
	studioID := os.Getenv("STUDIOFORGE_STUDIO_ID")
	if studioID == "" {
		if len(listing.Studios) != 1 {
			t.Fatalf("Studio MCP reported %d instances; set STUDIOFORGE_STUDIO_ID for explicit selection", len(listing.Studios))
		}
		studioID = listing.Studios[0].ID
	}
	if err := client.SelectStudio(ctx, studioID); err != nil {
		t.Fatal(err)
	}
	state, err := client.Call(ctx, "get_studio_state", nil)
	if err != nil {
		t.Fatal(err)
	}
	stateText, err := TextResult(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("live Studio MCP tools=%d selected=%s state=%s", len(tools), studioID, stateText)
}
