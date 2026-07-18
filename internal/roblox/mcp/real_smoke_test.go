package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

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
