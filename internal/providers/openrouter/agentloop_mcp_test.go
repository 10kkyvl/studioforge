package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
)

type fakeStudioTransport struct {
	tools   []mcp.Tool
	results map[string]json.RawMessage

	mu    sync.Mutex
	calls []string
}

func (f *fakeStudioTransport) ListTools(context.Context) ([]mcp.Tool, error) { return f.tools, nil }

func (f *fakeStudioTransport) Call(_ context.Context, name string, _ map[string]any) (json.RawMessage, error) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	f.mu.Unlock()
	if raw, ok := f.results[name]; ok {
		return raw, nil
	}
	return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
}

func (f *fakeStudioTransport) Close() error { return nil }

func (f *fakeStudioTransport) sawCall(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == name {
			return true
		}
	}
	return false
}

func TestAgentLoop_StudioToolCallSucceeds(t *testing.T) {
	dir := t.TempDir()
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{
				Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
					{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "script_read", Arguments: `{"path":"ServerScriptService/Main"}`}},
				}}, FinishReason: "tool_calls"}},
			}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "Read the script."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)

	transport := &fakeStudioTransport{
		tools: []mcp.Tool{{Name: "script_read"}},
		results: map[string]json.RawMessage{
			"script_read": json.RawMessage(`{"content":[{"type":"text","text":"print(\"hi\")"}]}`),
		},
	}
	provider.SetMCPConnector(func(ctx context.Context, projectID, runID, permissionProfile string) MCPGrant {
		client := mcp.NewClient(transport)
		return MCPGrant{
			Client:       client,
			AllowedTools: mcp.AllowedTools(permissionProfile),
			Release:      func() { _ = client.Close() },
		}
	})

	req := providers.RunRequest{RunID: "run-studio-1", ProjectID: "p1", WorkingDirectory: dir, Prompt: "read the script", Model: "test-model", PermissionProfile: "read-only"}
	events, result := runProvider(t, provider, req)

	calls := findEvents(events, "tool", "tool.call")
	results := findEvents(events, "tool", "tool.result")
	if len(calls) != 1 || len(results) != 1 {
		t.Fatalf("want 1 tool.call + 1 tool.result, got %d/%d", len(calls), len(results))
	}
	payload, _ := results[0].Payload.(map[string]any)
	if payload["isError"] == true {
		t.Fatalf("studio tool result reported error: %+v", payload)
	}
	if !strings.Contains(fmt.Sprint(payload["result"]), "print") {
		t.Errorf("tool result = %v", payload["result"])
	}
	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if !transport.sawCall("script_read") {
		t.Error("fake transport did not see the script_read call")
	}
	body1 := log.body(1)
	msgs, _ := body1["messages"].([]any)
	found := false
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" && strings.Contains(fmt.Sprint(mm["content"]), "print") {
			found = true
		}
	}
	if !found {
		t.Errorf("second request did not carry the studio tool result: %+v", msgs)
	}
}

func TestAgentLoop_DeniedStudioToolNeverReachesTheTransport(t *testing.T) {
	dir := t.TempDir()
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{
				Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
					{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "execute_luau", Arguments: `{"code":"print(1)"}`}},
				}}, FinishReason: "tool_calls"}},
			}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "Could not execute; read-only."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)

	transport := &fakeStudioTransport{tools: []mcp.Tool{{Name: "execute_luau"}}}
	provider.SetMCPConnector(func(ctx context.Context, projectID, runID, permissionProfile string) MCPGrant {
		client := mcp.NewClient(transport)
		return MCPGrant{
			Client:       client,
			AllowedTools: mcp.AllowedTools(permissionProfile),
			Release:      func() { _ = client.Close() },
		}
	})

	req := providers.RunRequest{RunID: "run-studio-2", ProjectID: "p1", WorkingDirectory: dir, Prompt: "run some code", Model: "test-model", PermissionProfile: "read-only"}
	events, result := runProvider(t, provider, req)

	results := findEvents(events, "tool", "tool.result")
	if len(results) != 1 {
		t.Fatalf("want 1 tool.result, got %d", len(results))
	}
	payload, _ := results[0].Payload.(map[string]any)
	if payload["isError"] != true {
		t.Errorf("expected the denied studio tool call to report a controlled error, got %+v", payload)
	}
	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if transport.sawCall("execute_luau") {
		t.Error("a Studio tool denied by the permission profile must never reach the transport")
	}
}
