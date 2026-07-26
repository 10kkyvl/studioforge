package openrouter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/attachments"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
)

var tinyPNGBase64 = base64.StdEncoding.EncodeToString([]byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
})

// A screenshot used to go only into the model's own context and be discarded at
// the end of the turn, so the one participant who could not see what the agent
// saw was the operator — the only one who can say whether it actually looks
// right. It is now saved into the project's attachments and announced as a
// message the chat renders a thumbnail from.
func TestAgentLoop_StudioScreenshotReachesTheOperator(t *testing.T) {
	dir := t.TempDir()
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{
				Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
					{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "screen_capture", Arguments: `{}`}},
				}}, FinishReason: "tool_calls"}},
			}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "The shop renders correctly."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)

	transport := &fakeStudioTransport{
		tools: []mcp.Tool{{Name: "screen_capture"}},
		results: map[string]json.RawMessage{
			"screen_capture": json.RawMessage(`{"content":[{"type":"image","data":"` + tinyPNGBase64 + `","mimeType":"image/png"}]}`),
		},
	}
	provider.SetMCPConnector(func(ctx context.Context, projectID, runID, permissionProfile string) MCPGrant {
		client := mcp.NewClient(transport)
		return MCPGrant{Client: client, AllowedTools: mcp.AllowedTools(permissionProfile), Release: func() { _ = client.Close() }}
	})

	req := providers.RunRequest{RunID: "run-shot-1", ProjectID: "p1", WorkingDirectory: dir, Prompt: "check the shop", Model: "test-model", PermissionProfile: "read-only"}
	events, result := runProvider(t, provider, req)
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if !transport.sawCall("screen_capture") {
		t.Fatal("the screenshot tool was never called")
	}

	announcements := findEvents(events, "message", "openrouter.screenshot")
	if len(announcements) != 1 {
		t.Fatalf("want one screenshot announcement, got %d in %+v", len(announcements), events)
	}
	payload, _ := announcements[0].Payload.(map[string]any)
	text := fmt.Sprint(payload["text"])
	if !strings.HasPrefix(text, attachments.PromptHeader) {
		t.Fatalf("text = %q, want the attachments block the chat renders from", text)
	}
	ref := strings.TrimSpace(strings.SplitN(text, "\n- ", 2)[1])
	if !strings.HasPrefix(ref, attachments.Dir+"/") {
		t.Fatalf("ref = %q, want a path under %q", ref, attachments.Dir)
	}
	if !attachments.ValidRef(dir, ref) {
		t.Fatalf("ref = %q does not resolve inside the project's attachments directory", ref)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(ref))); err != nil {
		t.Fatalf("the announced screenshot is not on disk: %v", err)
	}
}

// An ordinary text tool result must not produce a screenshot message, or every
// run would sprout empty image bubbles.
func TestAgentLoop_TextToolResultAnnouncesNoScreenshot(t *testing.T) {
	dir := t.TempDir()
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{
				Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
					{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "get_console_output", Arguments: `{}`}},
				}}, FinishReason: "tool_calls"}},
			}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "Console is clean."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	transport := &fakeStudioTransport{tools: []mcp.Tool{{Name: "get_console_output"}}}
	provider.SetMCPConnector(func(ctx context.Context, projectID, runID, permissionProfile string) MCPGrant {
		client := mcp.NewClient(transport)
		return MCPGrant{Client: client, AllowedTools: mcp.AllowedTools(permissionProfile), Release: func() { _ = client.Close() }}
	})

	req := providers.RunRequest{RunID: "run-shot-2", ProjectID: "p1", WorkingDirectory: dir, Prompt: "check the console", Model: "test-model", PermissionProfile: "read-only"}
	events, _ := runProvider(t, provider, req)
	if got := findEvents(events, "message", "openrouter.screenshot"); len(got) != 0 {
		t.Fatalf("a text tool result announced %d screenshots", len(got))
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(attachments.Dir))); !os.IsNotExist(err) {
		t.Errorf("an attachments directory was created with nothing to put in it: %v", err)
	}
}
