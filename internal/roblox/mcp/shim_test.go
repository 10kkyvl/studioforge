package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serveShim runs the shim over the given requests and returns one decoded
// response per request that carried an id.
func serveShim(t *testing.T, opts ShimOptions, requests ...string) []rpcResponse {
	t.Helper()
	var out strings.Builder
	if err := Serve(context.Background(), strings.NewReader(strings.Join(requests, "\n")+"\n"), &out, opts); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var responses []rpcResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var response rpcResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	return responses
}

func dialFake(transport Transport) Dialer {
	return func(context.Context, LaunchConfig) (Transport, error) { return transport, nil }
}

// The whole point of the shim: the launcher advertises nothing, and the agent
// is still told which tools exist.
func TestShimAdvertisesToolsWhenTheLauncherWillNot(t *testing.T) {
	responses := serveShim(t,
		ShimOptions{Dial: dialFake(&silentTransport{})},
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if len(responses) != 1 {
		t.Fatalf("responses=%d", len(responses))
	}
	var body struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(responses[0].Result, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tools) != len(OfficialTools) {
		t.Fatalf("tools=%d, want the %d known Studio tools", len(body.Tools), len(OfficialTools))
	}
	for _, tool := range body.Tools {
		if tool.InputSchema == nil {
			t.Fatalf("tool %q has no schema, so an agent cannot call it", tool.Name)
		}
	}
}

// When the launcher does publish tools, its own schemas win over the fallback
// and are remembered for the runs that come up secondary later.
func TestShimPrefersAndCachesPublishedTools(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "tools.json")
	responses := serveShim(t,
		ShimOptions{Dial: dialFake(&studioTransport{}), CachePath: cache},
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	var body struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(responses[0].Result, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tools) != 2 || body.Tools[0].Name != "list_roblox_studios" {
		t.Fatalf("tools=%v, want the launcher's own list", body.Tools)
	}

	// A later run whose launcher publishes nothing reuses the cached schemas
	// rather than falling back to the bare names.
	cached := serveShim(t,
		ShimOptions{Dial: dialFake(&silentTransport{}), CachePath: cache},
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	var reused struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(cached[0].Result, &reused); err != nil {
		t.Fatal(err)
	}
	if len(reused.Tools) != 2 || reused.Tools[0].Name != "list_roblox_studios" {
		t.Fatalf("tools=%v, want the cached list", reused.Tools)
	}
}

func TestShimForwardsCallsVerbatim(t *testing.T) {
	transport := &studioTransport{instances: []Instance{{ID: "one", Name: "place.rbxl"}}}
	responses := serveShim(t,
		ShimOptions{Dial: dialFake(transport)},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_roblox_studios","arguments":{}}}`)
	text, err := TextResult(responses[0].Result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "place.rbxl") {
		t.Fatalf("result=%q, want the launcher's own payload", text)
	}
}

// A tool the launcher rejects must reach the agent as that same rejection, so
// the agent can tell a missing tool from a failing one.
func TestShimPreservesLauncherErrors(t *testing.T) {
	transport := &studioTransport{callErr: &Error{Code: codeMethodNotFound, Message: "Method not found"}}
	responses := serveShim(t,
		ShimOptions{Dial: dialFake(transport)},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"execute_luau","arguments":{}}}`)
	if responses[0].Error == nil || responses[0].Error.Code != codeMethodNotFound {
		t.Fatalf("error=%+v, want method-not-found passed through", responses[0].Error)
	}
}

// A launcher that will not start must be reported, not hung on.
func TestShimReportsALauncherThatCannotStart(t *testing.T) {
	responses := serveShim(t,
		ShimOptions{Dial: func(context.Context, LaunchConfig) (Transport, error) {
			return nil, errors.New("Studio is not installed")
		}},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"script_read","arguments":{}}}`)
	if responses[0].Error == nil || !strings.Contains(responses[0].Error.Message, "Studio is not installed") {
		t.Fatalf("error=%+v, want the launcher failure", responses[0].Error)
	}
}

func TestShimHandshakeAndNotifications(t *testing.T) {
	responses := serveShim(t,
		ShimOptions{Dial: dialFake(&studioTransport{})},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"nonsense/method"}`)
	// The notification must draw no reply at all; three requests, three answers.
	if len(responses) != 3 {
		t.Fatalf("responses=%d, want 3 — a notification must not be answered", len(responses))
	}
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(responses[0].Result, &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.ProtocolVersion != protocolVersion {
		t.Fatalf("protocolVersion=%q", initialized.ProtocolVersion)
	}
	if responses[2].Error == nil || responses[2].Error.Code != codeMethodNotFound {
		t.Fatalf("error=%+v, want method-not-found for an unknown method", responses[2].Error)
	}
}

// The generated run config must launch StudioForge's shim, not the launcher.
func TestProvisionPointsTheAgentAtTheShim(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one", Name: "place.rbxl"}}})
	p.Exe = func() (string, error) { return "C:\\StudioForge.exe", nil }
	grant := p.Provision(context.Background(), "run-shim", "workspace-write", Target{})
	if grant.ConfigPath == "" {
		t.Fatalf("no access: %q", grant.Notice)
	}
	var config Config
	body, err := os.ReadFile(grant.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	launch := config.MCPServers[ServerName]
	if launch.Command != "C:\\StudioForge.exe" || len(launch.Args) == 0 || launch.Args[0] != "mcp-shim" {
		t.Fatalf("launch=%+v, want the shim subcommand", launch)
	}
	if !strings.Contains(strings.Join(launch.Args, " "), "--tool-cache") {
		t.Fatalf("args=%v, want a tool cache so schemas survive between runs", launch.Args)
	}
}

// Without a locatable executable the agent still gets the launcher directly,
// which is the behaviour that predates the shim.
func TestProvisionFallsBackToTheLauncher(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one", Name: "place.rbxl"}}})
	p.Exe = func() (string, error) { return "", errors.New("no executable") }
	grant := p.Provision(context.Background(), "run-fallback", "workspace-write", Target{})
	if grant.ConfigPath == "" {
		t.Fatalf("no access: %q", grant.Notice)
	}
	var config Config
	body, err := os.ReadFile(grant.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	if launch := config.MCPServers[ServerName]; len(launch.Args) > 0 && launch.Args[0] == "mcp-shim" {
		t.Fatalf("launch=%+v, want the raw launcher", launch)
	}
}
