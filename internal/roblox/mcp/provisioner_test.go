package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// studioTransport reports a configurable set of Studio instances.
type studioTransport struct {
	instances []Instance
	callErr   error
}

func (s *studioTransport) ListTools(context.Context) ([]Tool, error) {
	return []Tool{{Name: "list_roblox_studios"}, {Name: "script_read"}}, nil
}
func (s *studioTransport) Call(_ context.Context, name string, _ map[string]any) (json.RawMessage, error) {
	if s.callErr != nil {
		return nil, s.callErr
	}
	if name != "list_roblox_studios" {
		return json.RawMessage(`{"content":[{"type":"text","text":"{}"}]}`), nil
	}
	listing, err := json.Marshal(map[string]any{"studios": s.instances})
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": string(listing)}}})
	if err != nil {
		return nil, err
	}
	return body, nil
}
func (s *studioTransport) Close() error { return nil }

// newProvisioner points the launcher at a real file so DetectLauncher succeeds
// on any OS, and dials a fake transport instead of spawning Studio.
func newProvisioner(t *testing.T, transport Transport) *Provisioner {
	t.Helper()
	dir := t.TempDir()
	launcher := filepath.Join(dir, "mcp-launcher")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Provisioner{
		Dir:      dir,
		Override: func() string { return launcher },
		Dial:     func(context.Context, LaunchConfig) (Transport, error) { return transport, nil },
	}
}

func TestProvisionGrantsAccessForASingleStudio(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}})
	grant := p.Provision(context.Background(), "run-1", "workspace-write", Target{})
	if grant.ConfigPath == "" {
		t.Fatalf("expected access, got notice %q", grant.Notice)
	}
	body, err := os.ReadFile(grant.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	if _, ok := config.MCPServers[ServerName]; !ok {
		t.Errorf("generated config lacks the %s server: %s", ServerName, body)
	}
	if len(grant.AllowedTools) == 0 {
		t.Error("granting access without an allowlist leaves every tool call denied")
	}
	grant.Release()
	if _, err := os.Stat(grant.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("Release must delete the generated config, stat err=%v", err)
	}
}

// StudioForge cannot pin the instance on the agent's own MCP connection, so
// more than one open Studio must mean no access rather than a coin flip.
func TestProvisionRefusesAmbiguousStudioSelection(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one"}, {ID: "two"}}})
	grant := p.Provision(context.Background(), "run-1", "workspace-write", Target{})
	if grant.ConfigPath != "" {
		t.Fatal("two open Studios must not receive access")
	}
	if !strings.Contains(grant.Notice, "2 Studio instances") {
		t.Errorf("notice should say why access was withheld, got %q", grant.Notice)
	}
	entries, err := os.ReadDir(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			t.Errorf("no config may be written when access is refused, found %q", entry.Name())
		}
	}
}

func TestProvisionWithoutStudioIsNotAFailure(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	grant := p.Provision(context.Background(), "run-1", "workspace-write", Target{})
	if grant.ConfigPath != "" || grant.Notice != "" {
		t.Errorf("no Studio open should be silent, got path=%q notice=%q", grant.ConfigPath, grant.Notice)
	}
}

// Roblox hands the MCP host slot to one client at a time. A second client still
// connects and still answers, but lists no instances — indistinguishable here
// from a machine with Studio closed. Staying silent left the agent with no
// Studio tools and no reason, so it wrote files and claimed Rojo would sync.
func TestProvisionExplainsAStudioHeldByAnotherClient(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	p.Running = func(context.Context) bool { return true }
	grant := p.Provision(context.Background(), "run-1", "workspace-write", Target{})
	if grant.ConfigPath != "" {
		t.Fatal("a Studio held by another client must not receive access")
	}
	if !strings.Contains(grant.Notice, "another MCP client") {
		t.Errorf("notice must name the cause the operator can act on, got %q", grant.Notice)
	}
}

// The same empty list without a Studio process is the ordinary "nothing open"
// case, which must stay silent.
func TestProvisionStaysSilentWhenNoStudioProcessRuns(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	p.Running = func(context.Context) bool { return false }
	if grant := p.Provision(context.Background(), "run-1", "workspace-write", Target{}); grant.Notice != "" {
		t.Errorf("a closed Studio must not be reported as blocked, got %q", grant.Notice)
	}
}

// The badge needs the two apart too: reporting a held connection as "none" sent
// operators to reopen a Studio that was already in front of them.
func TestStatusReportsAHeldConnectionAsBlocked(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	p.Running = func(context.Context) bool { return true }
	status, err := p.Status(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if status.Open != 0 || !status.Blocked {
		t.Errorf("expected open=0 blocked=true, got open=%d blocked=%v", status.Open, status.Blocked)
	}
}

func TestProvisionMissingLauncherIsNotAFailure(t *testing.T) {
	p := newProvisioner(t, &studioTransport{})
	p.Override = func() string { return filepath.Join(t.TempDir(), "absent") }
	grant := p.Provision(context.Background(), "run-1", "workspace-write", Target{})
	if grant.ConfigPath != "" || grant.Notice != "" {
		t.Errorf("an absent launcher is an ordinary setup, got path=%q notice=%q", grant.ConfigPath, grant.Notice)
	}
}

func TestProvisionSurfacesLauncherErrors(t *testing.T) {
	p := newProvisioner(t, &studioTransport{callErr: errors.New("launcher exploded")})
	grant := p.Provision(context.Background(), "run-1", "workspace-write", Target{})
	if grant.ConfigPath != "" {
		t.Fatal("a broken launcher must not receive access")
	}
	if !strings.Contains(grant.Notice, "launcher exploded") {
		t.Errorf("notice should carry the cause, got %q", grant.Notice)
	}
}

// A read-only agent must not be handed the tools that rewrite the place.
func TestProvisionScopesToolsToTheProfile(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one"}}})
	grant := p.Provision(context.Background(), "run-1", "read-only", Target{})
	if grant.ConfigPath == "" {
		t.Fatalf("read-only should still get access, notice=%q", grant.Notice)
	}
	for _, tool := range grant.AllowedTools {
		if tool == ToolPrefix+"execute_luau" || tool == ToolPrefix+"multi_edit" {
			t.Errorf("read-only run was granted %q", tool)
		}
	}
}

// attachingTransport answers list_roblox_studios with the launcher's "Not
// connected to the WS host" tool error until the plugin attaches, which in a
// real handoff takes a second or two after the launcher spawns.
type attachingTransport struct {
	studioTransport
	failures int // calls to fail before the plugin attaches; negative fails forever
}

func (a *attachingTransport) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	if name == "list_roblox_studios" && a.failures != 0 {
		if a.failures > 0 {
			a.failures--
		}
		return json.RawMessage(`{"content":[{"type":"text","text":"Not connected to the WS host"}],"isError":true}`), nil
	}
	return a.studioTransport.Call(ctx, name, args)
}

// The plugin attaches to a freshly spawned launcher a beat after the probe's
// first call, so a single immediate listing withheld Studio on every run.
func TestProbeWaitsForThePluginToAttach(t *testing.T) {
	p := newProvisioner(t, &attachingTransport{
		studioTransport: studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		failures:        2,
	})
	p.Running = func(context.Context) bool { return true }
	p.retryEvery = 10 * time.Millisecond
	grant := p.Provision(context.Background(), "run-attach", "workspace-write", Target{})
	if grant.ConfigPath == "" {
		t.Fatalf("Studio withheld though the plugin attached after a retry: %q", grant.Notice)
	}
}

// A plugin that never attaches while Studio runs means another client holds the
// WS host; the notice must say so instead of echoing the raw launcher error.
func TestProvisionExplainsAPluginThatNeverAttaches(t *testing.T) {
	p := newProvisioner(t, &attachingTransport{failures: -1})
	p.Running = func(context.Context) bool { return true }
	p.attachWindow = 100 * time.Millisecond
	p.retryEvery = 10 * time.Millisecond
	grant := p.Provision(context.Background(), "run-held", "workspace-write", Target{})
	if grant.ConfigPath != "" {
		t.Fatal("an unreachable WS host must not receive access")
	}
	if !strings.Contains(grant.Notice, "another MCP client") {
		t.Errorf("notice must name the cause the operator can act on, got %q", grant.Notice)
	}
}

// The same launcher error with no Studio process is the ordinary "Studio
// closed" case: stay silent and do not sit out the attach window.
func TestProvisionStaysSilentWhenNotConnectedAndStudioClosed(t *testing.T) {
	p := newProvisioner(t, &attachingTransport{failures: -1})
	p.Running = func(context.Context) bool { return false }
	start := time.Now()
	grant := p.Provision(context.Background(), "run-closed", "workspace-write", Target{})
	if grant.ConfigPath != "" || grant.Notice != "" {
		t.Errorf("a closed Studio must stay silent, got path=%q notice=%q", grant.ConfigPath, grant.Notice)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("a closed Studio must not wait out the attach window")
	}
}

// The badge must show the held connection, not an error.
func TestStatusReportsAnUnattachedPluginAsBlocked(t *testing.T) {
	p := newProvisioner(t, &attachingTransport{failures: -1})
	p.Running = func(context.Context) bool { return true }
	p.attachWindow = 100 * time.Millisecond
	p.retryEvery = 10 * time.Millisecond
	status, err := p.Status(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if status.Open != 0 || !status.Blocked {
		t.Errorf("expected open=0 blocked=true, got open=%d blocked=%v", status.Open, status.Blocked)
	}
}

// hangingTransport accepts the connection and then never answers, which is what
// a Studio busy compiling or showing a modal looks like.
type hangingTransport struct{ ctxSeen chan struct{} }

func (h *hangingTransport) ListTools(ctx context.Context) ([]Tool, error) {
	select {
	case h.ctxSeen <- struct{}{}:
	default:
	}
	<-ctx.Done() // Only a deadline can release this.
	return nil, ctx.Err()
}
func (h *hangingTransport) Call(ctx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (h *hangingTransport) Close() error { return nil }

// Provision runs before the agent starts, holding a scheduler slot and the
// project write lease, and the run context carries no deadline of its own. An
// unresponsive Studio must cost the run its Studio access, not the run itself.
func TestProvisionDoesNotHangOnAnUnresponsiveStudio(t *testing.T) {
	p := newProvisioner(t, &hangingTransport{ctxSeen: make(chan struct{}, 1)})
	p.Timeout = 300 * time.Millisecond

	// context.Background() deliberately: a run's context carries no deadline, so
	// only the provisioner's own timeout can end this. Passing a ctx with a
	// deadline here would test the caller instead of the code under test.
	done := make(chan Grant, 1)
	go func() { done <- p.Provision(context.Background(), "run-1", "workspace-write", Target{}) }()
	select {
	case grant := <-done:
		if grant.ConfigPath != "" {
			t.Fatal("an unresponsive Studio must not receive access")
		}
		if grant.Notice == "" {
			t.Error("withholding access should say why")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Provision hung on an unresponsive Studio; a run would hold its slot and project lease forever")
	}
}

func TestProvisionRefusesUnknownProfile(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one"}}})
	grant := p.Provision(context.Background(), "run-1", "", Target{})
	if grant.ConfigPath != "" {
		t.Fatal("an unrecognised profile must not receive access")
	}
	if !strings.Contains(grant.Notice, "grants no Studio tools") {
		t.Errorf("notice=%q", grant.Notice)
	}
}

// silentTransport advertises no tools at all while answering every call, which
// is exactly how the launcher behaves when another MCP client won the WS host
// port: the tool list is pushed only to the host, but calls are forwarded.
type silentTransport struct{ studioTransport }

func (s *silentTransport) ListTools(context.Context) ([]Tool, error) { return nil, nil }

func TestProvisionIgnoresAnEmptyToolList(t *testing.T) {
	p := newProvisioner(t, &silentTransport{studioTransport{instances: []Instance{{ID: "one", Name: "place.rbxl"}}}})
	grant := p.Provision(context.Background(), "run-silent", "workspace-write", Target{})
	if grant.ConfigPath == "" {
		t.Fatalf("Studio was withheld though the instance listing answered: %q", grant.Notice)
	}
}

func TestCountOpenIgnoresAnEmptyToolList(t *testing.T) {
	p := newProvisioner(t, &silentTransport{studioTransport{instances: []Instance{{ID: "one", Name: "place.rbxl"}}}})
	open, err := p.CountOpen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if open != 1 {
		t.Fatalf("open=%d, want 1", open)
	}
}

// Only a method-not-found answer means the Studio is too old to list instances.
func TestProbeReportsAnOutdatedStudio(t *testing.T) {
	p := newProvisioner(t, &studioTransport{callErr: &Error{Code: codeMethodNotFound, Message: "Method not found"}})
	if _, err := p.CountOpen(context.Background()); err == nil || !strings.Contains(err.Error(), "update Roblox Studio") {
		t.Fatalf("err=%v, want an update-Studio error", err)
	}
}

// Any other call failure must surface as itself, not as advice to update.
func TestProbeSurfacesOrdinaryCallFailures(t *testing.T) {
	p := newProvisioner(t, &studioTransport{callErr: errors.New("studio busy")})
	_, err := p.CountOpen(context.Background())
	if err == nil || strings.Contains(err.Error(), "update Roblox Studio") {
		t.Fatalf("err=%v, want the underlying failure", err)
	}
}

// openingTransport reports nothing until Open is called, then reports the place
// that was opened — the shape of a real auto-open.
type openingTransport struct {
	studioTransport
	place string
}

func (o *openingTransport) open() { o.instances = []Instance{{ID: "opened", Name: o.place}} }

func TestProvisionPicksTheStudioHoldingThisProjectsPlace(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{
		{ID: "other", Name: "someone-elses-b2c3d4e5.rbxl"},
		{ID: "mine", Name: "my-game-a1b2c3d4.rbxl"},
	}})
	grant := p.Provision(context.Background(), "run-match", "workspace-write", Target{PlaceName: "my-game-a1b2c3d4.rbxl"})
	if grant.ConfigPath == "" {
		t.Fatalf("the project's own Studio was refused: %q", grant.Notice)
	}
}

// Two Studios open, neither this project's: granting either would let the run
// edit the wrong place.
func TestProvisionRefusesAnotherProjectsStudio(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "other", Name: "someone-elses-b2c3d4e5.rbxl"}}})
	grant := p.Provision(context.Background(), "run-foreign", "workspace-write",
		Target{PlaceName: "my-game-a1b2c3d4.rbxl"})
	if grant.ConfigPath != "" {
		t.Fatal("granted access to another project's Studio")
	}
	if !strings.Contains(grant.Notice, "does not hold this project's place") {
		t.Fatalf("notice=%q", grant.Notice)
	}
}

func TestProvisionOpensTheProjectsPlaceWhenNoneIsOpen(t *testing.T) {
	transport := &openingTransport{place: "my-game-a1b2c3d4.rbxl"}
	p := newProvisioner(t, transport)
	opened := false
	grant := p.Provision(context.Background(), "run-open", "workspace-write", Target{
		PlaceName: "my-game-a1b2c3d4.rbxl",
		Open: func(context.Context) error {
			opened = true
			transport.open()
			return nil
		},
	})
	if !opened {
		t.Fatal("Studio was never opened")
	}
	if grant.ConfigPath == "" {
		t.Fatalf("no access after opening: %q", grant.Notice)
	}
}

func TestProvisionLeavesStudioClosedWhenAutoOpenIsOff(t *testing.T) {
	transport := &openingTransport{place: "my-game-a1b2c3d4.rbxl"}
	p := newProvisioner(t, transport)
	p.AutoOpen = func() bool { return false }
	grant := p.Provision(context.Background(), "run-manual", "workspace-write", Target{
		PlaceName: "my-game-a1b2c3d4.rbxl",
		Open:      func(context.Context) error { t.Fatal("opened Studio though auto-open is off"); return nil },
	})
	if grant.ConfigPath != "" {
		t.Fatal("granted access with no Studio open")
	}
}

// A failure to open is the operator's problem to see, not something to hide.
func TestProvisionReportsAFailedOpen(t *testing.T) {
	p := newProvisioner(t, &studioTransport{})
	grant := p.Provision(context.Background(), "run-openfail", "workspace-write", Target{
		PlaceName: "my-game-a1b2c3d4.rbxl",
		Open:      func(context.Context) error { return errors.New("rojo build failed") },
	})
	if !strings.Contains(grant.Notice, "rojo build failed") {
		t.Fatalf("notice=%q, want the underlying failure", grant.Notice)
	}
}

// Other Studio instances being open is not the same as none being open at
// all: auto-opening on top of them risks piling a second window onto Studio
// rather than the one this project wants.
func TestProvisionNeverAutoOpensWhileAnotherInstanceIsOpen(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "other", Name: "someone-elses-b2c3d4e5.rbxl"}}})
	opened := 0
	grant := p.Provision(context.Background(), "run-noauto", "workspace-write", Target{
		PlaceName: "my-game-a1b2c3d4.rbxl",
		Open:      func(context.Context) error { opened++; return nil },
	})
	if opened != 0 {
		t.Fatalf("Studio was opened though another instance is already up, opened=%d", opened)
	}
	if grant.ConfigPath != "" {
		t.Fatal("granted access though no instance holds this project's place")
	}
	if !strings.Contains(grant.Notice, "does not hold this project's place") {
		t.Fatalf("notice=%q", grant.Notice)
	}
	if !strings.Contains(grant.Notice, "someone-elses-b2c3d4e5.rbxl") || !strings.Contains(grant.Notice, "my-game-a1b2c3d4.rbxl") {
		t.Fatalf("notice should name what is open next to what was expected, got %q", grant.Notice)
	}
}

// Restates TestProvisionOpensTheProjectsPlaceWhenNoneIsOpen with an explicit
// call count rather than a bool, since the point being guarded against is a
// second, duplicate launch, not merely "at least one".
func TestProvisionOpensExactlyOnceWhenNothingIsOpen(t *testing.T) {
	transport := &openingTransport{place: "my-game-a1b2c3d4.rbxl"}
	p := newProvisioner(t, transport)
	opens := 0
	grant := p.Provision(context.Background(), "run-open-once", "workspace-write", Target{
		PlaceName: "my-game-a1b2c3d4.rbxl",
		Open: func(context.Context) error {
			opens++
			transport.open()
			return nil
		},
	})
	if opens != 1 {
		t.Fatalf("opens=%d, want exactly 1", opens)
	}
	if grant.ConfigPath == "" {
		t.Fatalf("no access after opening: %q", grant.Notice)
	}
}

// PlaceName is meant to be unique per project, so two instances both
// reporting it should not happen in practice — but if it did, the match must
// still be refused rather than picked from arbitrarily, the same fail-closed
// rule every other ambiguous case in this package already follows.
func TestProvisionRefusesAmbiguousTargetMatch(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{
		{ID: "one", Name: "my-game-a1b2c3d4.rbxl"},
		{ID: "two", Name: "my-game-a1b2c3d4.rbxl"},
	}})
	grant := p.Provision(context.Background(), "run-ambiguous-target", "workspace-write", Target{PlaceName: "my-game-a1b2c3d4.rbxl"})
	if grant.ConfigPath != "" {
		t.Fatal("two instances holding the same place must not receive access")
	}
	if !strings.Contains(grant.Notice, "2 Studio instances hold my-game-a1b2c3d4.rbxl") {
		t.Fatalf("notice=%q", grant.Notice)
	}
}

func TestCheckOpenReportsSafeWhenNothingIsOpen(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	check := p.CheckOpen(context.Background(), "my-game-a1b2c3d4.rbxl")
	if !check.Open || check.Matched || check.Notice != "" {
		t.Fatalf("check=%+v, want Open=true only", check)
	}
}

func TestCheckOpenReportsMatchedWhenThisProjectsPlaceIsAlreadyOpen(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "mine", Name: "my-game-a1b2c3d4.rbxl"}}})
	check := p.CheckOpen(context.Background(), "my-game-a1b2c3d4.rbxl")
	if check.Open || !check.Matched || check.Notice != "" {
		t.Fatalf("check=%+v, want Matched=true only", check)
	}
}

func TestCheckOpenRefusesWhenOtherInstancesAreOpen(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "other", Name: "someone-elses-b2c3d4e5.rbxl"}}})
	check := p.CheckOpen(context.Background(), "my-game-a1b2c3d4.rbxl")
	if check.Open || check.Matched {
		t.Fatalf("check=%+v, want neither Open nor Matched", check)
	}
	if !strings.Contains(check.Notice, "someone-elses-b2c3d4e5.rbxl") || !strings.Contains(check.Notice, "my-game-a1b2c3d4.rbxl") {
		t.Fatalf("notice should name what is open next to what was expected, got %q", check.Notice)
	}
}

// Two instances both somehow reporting this project's place must still read
// as "do not launch" rather than be picked from arbitrarily — a third window
// would only make the ambiguity worse, never resolve it.
func TestCheckOpenTreatsAnAmbiguousMatchAsAlreadyOpenRatherThanLaunching(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{
		{ID: "one", Name: "my-game-a1b2c3d4.rbxl"},
		{ID: "two", Name: "my-game-a1b2c3d4.rbxl"},
	}})
	check := p.CheckOpen(context.Background(), "my-game-a1b2c3d4.rbxl")
	if check.Open {
		t.Fatal("an ambiguous match must never be reported as safe to launch")
	}
	if !check.Matched {
		t.Fatalf("check=%+v, want Matched=true so the caller does not launch a third window", check)
	}
}

func TestCheckOpenFailsOpenWithNoLauncherConfigured(t *testing.T) {
	p := newProvisioner(t, &studioTransport{})
	p.Override = func() string { return filepath.Join(t.TempDir(), "absent") }
	check := p.CheckOpen(context.Background(), "my-game-a1b2c3d4.rbxl")
	if !check.Open {
		t.Fatalf("check=%+v, want Open=true when there is nothing to probe", check)
	}
}

func TestCheckOpenIgnoresAnEmptyPlaceName(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one", Name: "whatever.rbxl"}}})
	check := p.CheckOpen(context.Background(), "")
	if !check.Open {
		t.Fatalf("check=%+v, want Open=true with no place to check against", check)
	}
}

// A Studio whose WS host is owned by another MCP client lists no instances
// and errors nothing — indistinguishable, at the listing level, from no
// Studio at all. Auto-opening on that stacks a duplicate window onto the
// operator's already-open Studio, so the running-process tie-break must
// refuse the launch and say why.
func TestProvisionDoesNotAutoOpenOverAStudioHiddenByAnotherClient(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	p.Running = func(context.Context) bool { return true }
	grant := p.Provision(context.Background(), "run-host-taken", "workspace-write", Target{
		PlaceName: "my-game-a1b2c3d4.rbxl",
		Open:      func(context.Context) error { t.Fatal("launched a duplicate Studio over a host-taken one"); return nil },
	})
	if grant.ConfigPath != "" {
		t.Fatal("granted access though the WS host is owned by another client")
	}
	if !strings.Contains(grant.Notice, "another MCP client") {
		t.Fatalf("notice=%q, want the host-taken explanation", grant.Notice)
	}
}

func TestCheckOpenRefusesToLaunchOverAStudioHiddenByAnotherClient(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	p.Running = func(context.Context) bool { return true }
	check := p.CheckOpen(context.Background(), "my-game-a1b2c3d4.rbxl")
	if check.Open || check.Matched {
		t.Fatalf("check=%+v, want a refusal, not a launch", check)
	}
	if !strings.Contains(check.Notice, "another MCP client") {
		t.Fatalf("notice=%q, want the host-taken explanation", check.Notice)
	}
}

func TestCheckOpenStillReportsSafeWhenNoStudioProcessRuns(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	p.Running = func(context.Context) bool { return false }
	check := p.CheckOpen(context.Background(), "my-game-a1b2c3d4.rbxl")
	if !check.Open || check.Matched || check.Notice != "" {
		t.Fatalf("check=%+v, want Open=true only", check)
	}
}

// Matching is by place file name, which on Windows is case-insensitive.
func TestProvisionMatchesPlaceNamesCaseInsensitively(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "mine", Name: "My-Game-A1B2C3D4.rbxl"}}})
	grant := p.Provision(context.Background(), "run-case", "workspace-write",
		Target{PlaceName: "my-game-a1b2c3d4.rbxl"})
	if grant.ConfigPath == "" {
		t.Fatalf("case difference refused the project's own Studio: %q", grant.Notice)
	}
}
