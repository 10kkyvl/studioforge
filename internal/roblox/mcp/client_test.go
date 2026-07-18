package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

type fakeTransport struct{ calls []string }

func (f *fakeTransport) ListTools(context.Context) ([]Tool, error) {
	return []Tool{{Name: "list_roblox_studios"}, {Name: "set_active_studio"}, {Name: "start_stop_play"}}, nil
}
func (f *fakeTransport) Call(_ context.Context, name string, _ map[string]any) (json.RawMessage, error) {
	f.calls = append(f.calls, name)
	return json.RawMessage(`{"ok":true}`), nil
}
func (f *fakeTransport) Close() error { return nil }
func TestDiscoveryAndExplicitStudioSelection(t *testing.T) {
	transport := &fakeTransport{}
	client := NewClient(transport)
	tools, err := client.Discover(context.Background())
	if err != nil || len(tools) != 3 {
		t.Fatalf("tools=%d err=%v", len(tools), err)
	}
	if err := client.SelectStudio(context.Background(), ""); err == nil {
		t.Fatal("empty selection accepted")
	}
	if err := client.SelectStudio(context.Background(), "instance-1"); err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) != 1 || transport.calls[0] != "set_active_studio" {
		t.Fatalf("calls=%v", transport.calls)
	}
	// A tool missing from the advertised list must still be called. Only the
	// launcher holding the WS host port is pushed that list, so refusing here
	// would deny working calls whenever another MCP client is attached to
	// Studio — the defect this test previously locked in.
	if _, err := client.Call(context.Background(), "execute_luau", nil); err != nil {
		t.Fatalf("undiscovered tool refused: %v", err)
	}
	if len(transport.calls) != 2 || transport.calls[1] != "execute_luau" {
		t.Fatalf("calls=%v", transport.calls)
	}
}

// A Studio too old to implement a tool answers method-not-found, and that is
// the only failure callers may report as "update Roblox Studio".
func TestMethodNotFoundIsDistinguishable(t *testing.T) {
	notFound := &Error{Code: codeMethodNotFound, Message: "Method not found"}
	if !IsMethodNotFound(notFound) {
		t.Fatal("method-not-found not recognised")
	}
	if !IsMethodNotFound(fmt.Errorf("probe Studio: %w", notFound)) {
		t.Fatal("method-not-found not recognised through a wrapped error")
	}
	if IsMethodNotFound(&Error{Code: -32000, Message: "Studio busy"}) {
		t.Fatal("an ordinary call failure was read as method-not-found")
	}
	if IsMethodNotFound(errors.New("offline")) {
		t.Fatal("a transport failure was read as method-not-found")
	}
}
func TestDiscoveryFailure(t *testing.T) {
	client := NewClient(errorTransport{})
	if _, err := client.Discover(context.Background()); err == nil {
		t.Fatal("failure ignored")
	}
}

type errorTransport struct{}

func (errorTransport) ListTools(context.Context) ([]Tool, error) { return nil, errors.New("offline") }
func (errorTransport) Call(context.Context, string, map[string]any) (json.RawMessage, error) {
	return nil, errors.New("offline")
}
func (errorTransport) Close() error { return nil }
