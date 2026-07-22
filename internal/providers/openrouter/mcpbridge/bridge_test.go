package mcpbridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
)

type fakeTransport struct {
	tools   []mcp.Tool
	listErr error
	results map[string]json.RawMessage
	errs    map[string]error

	mu    sync.Mutex
	calls []string
}

func (f *fakeTransport) ListTools(context.Context) ([]mcp.Tool, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tools, nil
}

func (f *fakeTransport) Call(_ context.Context, name string, _ map[string]any) (json.RawMessage, error) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	f.mu.Unlock()
	if err, ok := f.errs[name]; ok {
		return nil, err
	}
	if raw, ok := f.results[name]; ok {
		return raw, nil
	}
	return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
}

func (f *fakeTransport) Close() error { return nil }

func (f *fakeTransport) sawCall(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == name {
			return true
		}
	}
	return false
}

func (f *fakeTransport) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestBridgeExecutesAnAllowedTool(t *testing.T) {
	transport := &fakeTransport{
		tools: []mcp.Tool{{Name: "script_read"}},
		results: map[string]json.RawMessage{
			"script_read": json.RawMessage(`{"content":[{"type":"text","text":"local x = 1"}]}`),
		},
	}
	client := mcp.NewClient(transport)
	b := New(context.Background(), client, mcp.AllowedTools("read-only"), 0)
	res := b.Execute(context.Background(), "script_read", json.RawMessage(`{"path":"a.lua"}`))
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if res.Content != "local x = 1" {
		t.Errorf("content = %q", res.Content)
	}
	if !transport.sawCall("script_read") {
		t.Error("expected the transport to see the script_read call")
	}
}

func TestBridgeDeniesAToolOutsideTheAllowlist(t *testing.T) {
	transport := &fakeTransport{tools: []mcp.Tool{{Name: "execute_luau"}}}
	client := mcp.NewClient(transport)
	b := New(context.Background(), client, mcp.AllowedTools("read-only"), 0)
	res := b.Execute(context.Background(), "execute_luau", json.RawMessage(`{}`))
	if !res.IsError || !strings.Contains(res.Content, "not permitted") {
		t.Fatalf("res = %+v", res)
	}
	if transport.callCount() != 0 {
		t.Errorf("denied tool must never reach the transport, calls=%v", transport.calls)
	}
}

func TestBridgeHasReflectsOnlyAdvertisedTools(t *testing.T) {
	transport := &fakeTransport{tools: []mcp.Tool{{Name: "script_read"}}}
	client := mcp.NewClient(transport)
	b := New(context.Background(), client, mcp.AllowedTools("read-only"), 0)
	if !b.Has("script_read") {
		t.Error("script_read should be advertised")
	}
	if b.Has("totally_unknown_tool") {
		t.Error("an unadvertised tool must report Has=false")
	}
}

func TestBridgeReportsMethodNotFoundAsAControlledError(t *testing.T) {
	transport := &fakeTransport{
		tools: []mcp.Tool{{Name: "script_read"}},
		errs:  map[string]error{"script_read": &mcp.Error{Code: -32601, Message: "Method not found"}},
	}
	client := mcp.NewClient(transport)
	b := New(context.Background(), client, mcp.AllowedTools("read-only"), 0)
	res := b.Execute(context.Background(), "script_read", json.RawMessage(`{}`))
	if !res.IsError || !strings.Contains(res.Content, "not available in this Studio") {
		t.Fatalf("res = %+v", res)
	}
}

func TestBridgeSurfacesOrdinaryCallFailuresAsControlledErrors(t *testing.T) {
	transport := &fakeTransport{
		tools: []mcp.Tool{{Name: "script_read"}},
		errs:  map[string]error{"script_read": errors.New("studio busy")},
	}
	client := mcp.NewClient(transport)
	b := New(context.Background(), client, mcp.AllowedTools("read-only"), 0)
	res := b.Execute(context.Background(), "script_read", json.RawMessage(`{}`))
	if !res.IsError || !strings.Contains(res.Content, "studio busy") {
		t.Fatalf("res = %+v", res)
	}
}

func TestBridgeCapsOversizedResults(t *testing.T) {
	big := strings.Repeat("a", 100)
	transport := &fakeTransport{
		tools: []mcp.Tool{{Name: "script_read"}},
		results: map[string]json.RawMessage{
			"script_read": json.RawMessage(`{"content":[{"type":"text","text":"` + big + `"}]}`),
		},
	}
	client := mcp.NewClient(transport)
	b := New(context.Background(), client, mcp.AllowedTools("read-only"), 10)
	res := b.Execute(context.Background(), "script_read", json.RawMessage(`{}`))
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.HasPrefix(res.Content, big[:10]) {
		t.Errorf("content = %q, want it to start with the first 10 bytes", res.Content)
	}
	if !strings.Contains(res.Content, "truncated") {
		t.Errorf("content = %q, want a truncation note", res.Content)
	}
	if len(res.Content) >= len(big) {
		t.Errorf("content length = %d, want it capped well below the original %d", len(res.Content), len(big))
	}
}

func TestTruncateProducesValidUTF8WhenTheCutLandsMidRune(t *testing.T) {
	s := strings.Repeat("б", 50)
	got := truncate(s, 51)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate produced invalid UTF-8: %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("content = %q, want a truncation note", got)
	}
}

func TestBridgeSurfacesToolReportedErrors(t *testing.T) {
	transport := &fakeTransport{
		tools: []mcp.Tool{{Name: "script_read"}},
		results: map[string]json.RawMessage{
			"script_read": json.RawMessage(`{"content":[{"type":"text","text":"bad path"}],"isError":true}`),
		},
	}
	client := mcp.NewClient(transport)
	b := New(context.Background(), client, mcp.AllowedTools("read-only"), 0)
	res := b.Execute(context.Background(), "script_read", json.RawMessage(`{}`))
	if !res.IsError {
		t.Fatalf("expected IsError true, got %+v", res)
	}
}

func TestBridgeReturnsStudioScreenshotWithoutLoggingBase64AsText(t *testing.T) {
	transport := &fakeTransport{
		tools: []mcp.Tool{{Name: "screen_capture"}},
		results: map[string]json.RawMessage{
			"screen_capture": json.RawMessage(`{"content":[{"type":"image","data":"/9j/example","mimeType":"image/jpeg"}]}`),
		},
	}
	b := New(context.Background(), mcp.NewClient(transport), mcp.AllowedTools("read-only"), 0)
	res := b.Execute(context.Background(), "screen_capture", json.RawMessage(`{}`))
	if res.IsError || res.ImageURL != "data:image/jpeg;base64,/9j/example" {
		t.Fatalf("result=%+v", res)
	}
	if strings.Contains(res.Content, "/9j/example") {
		t.Fatal("base64 leaked into textual tool output")
	}
}

func TestBridgeReturnsAControlledErrorForMalformedArguments(t *testing.T) {
	transport := &fakeTransport{tools: []mcp.Tool{{Name: "script_read"}}}
	client := mcp.NewClient(transport)
	b := New(context.Background(), client, mcp.AllowedTools("read-only"), 0)
	res := b.Execute(context.Background(), "script_read", json.RawMessage(`{not json`))
	if !res.IsError {
		t.Fatalf("expected IsError for malformed arguments, got %+v", res)
	}
	if transport.callCount() != 0 {
		t.Errorf("malformed arguments must never reach the transport, calls=%v", transport.calls)
	}
}

func TestNewAdvertisesOnlyTheIntersectionOfDiscoveredAndAllowed(t *testing.T) {
	transport := &fakeTransport{tools: []mcp.Tool{{Name: "script_read"}}}
	client := mcp.NewClient(transport)
	b := New(context.Background(), client, mcp.AllowedTools("workspace-write"), 0)
	names := b.Names()
	if len(names) != 1 || names[0] != "script_read" {
		t.Fatalf("names = %v, want only script_read", names)
	}
	if len(b.Definitions()) != 1 {
		t.Fatalf("definitions = %v, want 1", b.Definitions())
	}
}

func TestNewToleratesDiscoveryFailure(t *testing.T) {
	transport := &fakeTransport{listErr: errors.New("discovery boom")}
	client := mcp.NewClient(transport)
	b := New(context.Background(), client, mcp.AllowedTools("read-only"), 0)
	if len(b.Names()) != 0 {
		t.Fatalf("names = %v, want none when discovery fails", b.Names())
	}
}
