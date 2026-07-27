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
	if !grant.Studio {
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
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one", Name: "A.rbxl"}, {ID: "two", Name: "B.rbxl"}}})
	grant := p.Provision(context.Background(), "run-1", "workspace-write", Target{})
	if grant.Studio {
		t.Fatal("two open Studios must not receive access")
	}
	if !strings.Contains(grant.Notice, "2 Studio instances") {
		t.Errorf("notice should say why access was withheld, got %q", grant.Notice)
	}
	// A config is still written — every Claude run carries StudioForge's own
	// question server — but it must not name Studio, which is what was refused.
	body, err := os.ReadFile(grant.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	if _, ok := config.MCPServers[ServerName]; ok {
		t.Error("a refused Studio must not be registered in the run's config")
	}
	if _, ok := config.MCPServers[QuestionServerName]; !ok {
		t.Error("a run refused Studio still has to be able to ask the operator a question")
	}
}

func TestProvisionWithoutStudioIsNotAFailure(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	grant := p.Provision(context.Background(), "run-1", "workspace-write", Target{})
	if grant.Studio || grant.Notice != "" {
		t.Errorf("no Studio open should be silent, got studio=%v notice=%q", grant.Studio, grant.Notice)
	}
}

// Roblox hands the MCP host slot to one client at a time. A second client still
// connects and still answers, but lists no instances — indistinguishable here
// from a machine with Studio closed. Staying silent left the agent with no
// Studio tools and no reason, so it wrote files and claimed Rojo would sync.
func TestProvisionExplainsAStudioHeldByAnotherClient(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	p.Running = func(context.Context) bool { return true }
	p.attachWindow = 100 * time.Millisecond
	p.retryEvery = 10 * time.Millisecond
	grant := p.Provision(context.Background(), "run-1", "workspace-write", Target{})
	if grant.Studio {
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
	p.attachWindow = 100 * time.Millisecond
	p.retryEvery = 10 * time.Millisecond
	status, err := p.Status(context.Background(), Place{})
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
	if grant.Studio || grant.Notice != "" {
		t.Errorf("an absent launcher is an ordinary setup, got studio=%v notice=%q", grant.Studio, grant.Notice)
	}
}

func TestProvisionSurfacesLauncherErrors(t *testing.T) {
	p := newProvisioner(t, &studioTransport{callErr: errors.New("launcher exploded")})
	grant := p.Provision(context.Background(), "run-1", "workspace-write", Target{})
	if grant.Studio {
		t.Fatal("a broken launcher must not receive access")
	}
	if !strings.Contains(grant.Notice, "launcher exploded") {
		t.Errorf("notice should carry the cause, got %q", grant.Notice)
	}
}

// A read-only agent must not be handed the tools that rewrite the place.
func TestProvisionScopesToolsToTheProfile(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}})
	grant := p.Provision(context.Background(), "run-1", "read-only", Target{})
	if !grant.Studio {
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
	if !grant.Studio {
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
	if grant.Studio {
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
	if grant.Studio || grant.Notice != "" {
		t.Errorf("a closed Studio must stay silent, got studio=%v notice=%q", grant.Studio, grant.Notice)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("a closed Studio must not wait out the attach window")
	}
}

// registeringTransport reproduces the second half of the attach. Once the
// plugin has dialled the WS host the listing stops erroring, but Studio has not
// registered its window yet, so it succeeds with an empty list — the launcher
// answers `{"studios":[]}` for a beat before the place shows up.
type registeringTransport struct {
	studioTransport
	empty int // successful-but-empty listings before the place registers
}

func (r *registeringTransport) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	if name == "list_roblox_studios" && r.empty != 0 {
		if r.empty > 0 {
			r.empty--
		}
		return json.RawMessage(`{"content":[{"type":"text","text":"{\"studios\":[]}"}]}`), nil
	}
	return r.studioTransport.Call(ctx, name, args)
}

// Only the first half of the attach reports an error; the half where the host is
// up but the place has not registered answers successfully with an empty list.
// Stopping there handed an empty list back as final, and an empty list with
// Studio running is reported as another MCP client holding the connection — so
// a run started a beat early was refused Studio and told to close a client that
// did not exist.
func TestProbeWaitsForStudioToRegisterItsPlace(t *testing.T) {
	p := newProvisioner(t, &registeringTransport{
		studioTransport: studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		empty:           2,
	})
	p.Running = func(context.Context) bool { return true }
	p.retryEvery = 10 * time.Millisecond
	grant := p.Provision(context.Background(), "run-registering", "workspace-write", Target{})
	if !grant.Studio {
		t.Fatalf("Studio withheld though the place registered after a retry: %q", grant.Notice)
	}
}

// A place that never registers while Studio runs is still refused, so waiting
// out the attach cannot turn a genuine refusal into an unbounded wait.
func TestProvisionExplainsAPlaceThatNeverRegisters(t *testing.T) {
	p := newProvisioner(t, &registeringTransport{empty: -1})
	p.Running = func(context.Context) bool { return true }
	p.attachWindow = 100 * time.Millisecond
	p.retryEvery = 10 * time.Millisecond
	grant := p.Provision(context.Background(), "run-unregistered", "workspace-write", Target{})
	if grant.Studio {
		t.Fatal("a Studio that registered no place must not receive access")
	}
	if !strings.Contains(grant.Notice, "another MCP client") {
		t.Errorf("notice must name a cause the operator can act on, got %q", grant.Notice)
	}
}

// The same empty listing with no Studio process is the ordinary "Studio closed"
// case. It must stay silent and answer at once rather than sit out the window
// that now covers empty listings too.
func TestProvisionStaysSilentWhenListEmptyAndStudioClosed(t *testing.T) {
	p := newProvisioner(t, &registeringTransport{empty: -1})
	p.Running = func(context.Context) bool { return false }
	start := time.Now()
	grant := p.Provision(context.Background(), "run-empty-closed", "workspace-write", Target{})
	if grant.Studio || grant.Notice != "" {
		t.Errorf("a closed Studio must stay silent, got studio=%v notice=%q", grant.Studio, grant.Notice)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("a closed Studio must not wait out the attach window")
	}
}

// namelessTransport reproduces the last step of the attach: the instance is
// registered and listed, but Studio has not filled in the place it holds, so
// the name comes back empty.
type namelessTransport struct {
	studioTransport
	nameless int // listings carrying an unnamed instance before the place lands
}

func (n *namelessTransport) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	if name == "list_roblox_studios" && n.nameless != 0 {
		if n.nameless > 0 {
			n.nameless--
		}
		return json.RawMessage(`{"content":[{"type":"text","text":"{\"studios\":[{\"id\":\"one\",\"name\":\"\"}]}"}]}`), nil
	}
	return n.studioTransport.Call(ctx, name, args)
}

// An instance listed before its place name lands is not an empty list, so it
// sailed past the attach wait and into matching() — where it failed the place
// comparison and came back as "the open Studio does not hold this project's
// place … found (unnamed)", refusing the run over a mismatch that never was.
func TestProbeWaitsForStudioToNameItsPlace(t *testing.T) {
	p := newProvisioner(t, &namelessTransport{
		studioTransport: studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}},
		nameless:        2,
	})
	p.Running = func(context.Context) bool { return true }
	p.retryEvery = 10 * time.Millisecond
	grant := p.Provision(context.Background(), "run-nameless", "workspace-write", Target{Place: Place{FileName: "Place.rbxl"}})
	if !grant.Studio {
		t.Fatalf("Studio withheld though the place was named after a retry: %q", grant.Notice)
	}
}

// A different place that is genuinely open is not mid-registration: its name is
// there on the first listing, so the mismatch must still be reported at once and
// still say what is actually open.
func TestProvisionReportsAGenuineMismatchWithoutWaiting(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one", Name: "Other.rbxl"}}})
	p.Running = func(context.Context) bool { return true }
	start := time.Now()
	grant := p.Provision(context.Background(), "run-mismatch", "workspace-write", Target{Place: Place{FileName: "Place.rbxl"}})
	if grant.Studio {
		t.Fatal("a Studio holding a different place must not receive access")
	}
	if !strings.Contains(grant.Notice, "Other.rbxl") {
		t.Errorf("notice must name what is actually open, got %q", grant.Notice)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("a genuine mismatch waited %s; it is not a registration in progress", elapsed)
	}
}

// Waiting out the attach is right for a run, which keeps whatever access this
// decides for its whole length, but the badge polls Status every few seconds
// and the Open Studio button blocks on CheckOpen. A Studio that registers
// nothing holds that state for as long as it stays open, so spending the run
// gate's window on those would stall the UI on every poll and every click.
// This runs on the production constants deliberately: seeding attachWindow
// would override the very budget under test.
func TestGlancingCallersDoNotWaitOutTheRunGateWindow(t *testing.T) {
	for _, c := range []struct {
		name string
		call func(*Provisioner)
	}{
		{"Status", func(p *Provisioner) { _, _ = p.Status(context.Background(), Place{}) }},
		{"CheckOpen", func(p *Provisioner) { _ = p.CheckOpen(context.Background(), Place{FileName: "Place.rbxl"}) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := newProvisioner(t, &registeringTransport{empty: -1})
			p.Running = func(context.Context) bool { return true }
			start := time.Now()
			c.call(p)
			if elapsed := time.Since(start); elapsed >= attachWait {
				t.Errorf("%s took %s, the run gate's whole %s window; a glance must give up sooner", c.name, elapsed, attachWait)
			}
		})
	}
}

// The badge must show the held connection, not an error.
func TestStatusReportsAnUnattachedPluginAsBlocked(t *testing.T) {
	p := newProvisioner(t, &attachingTransport{failures: -1})
	p.Running = func(context.Context) bool { return true }
	p.attachWindow = 100 * time.Millisecond
	p.retryEvery = 10 * time.Millisecond
	status, err := p.Status(context.Background(), Place{})
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
		if grant.Studio {
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
	if grant.Studio {
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
	if !grant.Studio {
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
	grant := p.Provision(context.Background(), "run-match", "workspace-write", Target{Place: Place{FileName: "my-game-a1b2c3d4.rbxl"}})
	if !grant.Studio {
		t.Fatalf("the project's own Studio was refused: %q", grant.Notice)
	}
}

// Two Studios open, neither this project's: granting either would let the run
// edit the wrong place.
func TestProvisionRefusesAnotherProjectsStudio(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "other", Name: "someone-elses-b2c3d4e5.rbxl"}}})
	grant := p.Provision(context.Background(), "run-foreign", "workspace-write",
		Target{Place: Place{FileName: "my-game-a1b2c3d4.rbxl"}})
	if grant.Studio {
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
		Place: Place{FileName: "my-game-a1b2c3d4.rbxl"},
		Open: func(context.Context) error {
			opened = true
			transport.open()
			return nil
		},
	})
	if !opened {
		t.Fatal("Studio was never opened")
	}
	if !grant.Studio {
		t.Fatalf("no access after opening: %q", grant.Notice)
	}
}

func TestProvisionLeavesStudioClosedWhenAutoOpenIsOff(t *testing.T) {
	transport := &openingTransport{place: "my-game-a1b2c3d4.rbxl"}
	p := newProvisioner(t, transport)
	p.AutoOpen = func() bool { return false }
	grant := p.Provision(context.Background(), "run-manual", "workspace-write", Target{
		Place: Place{FileName: "my-game-a1b2c3d4.rbxl"},
		Open:  func(context.Context) error { t.Fatal("opened Studio though auto-open is off"); return nil },
	})
	if grant.Studio {
		t.Fatal("granted access with no Studio open")
	}
}

// A failure to open is the operator's problem to see, not something to hide.
func TestProvisionReportsAFailedOpen(t *testing.T) {
	p := newProvisioner(t, &studioTransport{})
	grant := p.Provision(context.Background(), "run-openfail", "workspace-write", Target{
		Place: Place{FileName: "my-game-a1b2c3d4.rbxl"},
		Open:  func(context.Context) error { return errors.New("rojo build failed") },
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
		Place: Place{FileName: "my-game-a1b2c3d4.rbxl"},
		Open:  func(context.Context) error { opened++; return nil },
	})
	if opened != 0 {
		t.Fatalf("Studio was opened though another instance is already up, opened=%d", opened)
	}
	if grant.Studio {
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
		Place: Place{FileName: "my-game-a1b2c3d4.rbxl"},
		Open: func(context.Context) error {
			opens++
			transport.open()
			return nil
		},
	})
	if opens != 1 {
		t.Fatalf("opens=%d, want exactly 1", opens)
	}
	if !grant.Studio {
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
	grant := p.Provision(context.Background(), "run-ambiguous-target", "workspace-write", Target{Place: Place{FileName: "my-game-a1b2c3d4.rbxl"}})
	if grant.Studio {
		t.Fatal("two instances holding the same place must not receive access")
	}
	if !strings.Contains(grant.Notice, "2 Studio instances hold my-game-a1b2c3d4.rbxl") {
		t.Fatalf("notice=%q", grant.Notice)
	}
}

func TestCheckOpenReportsSafeWhenNothingIsOpen(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	check := p.CheckOpen(context.Background(), Place{FileName: "my-game-a1b2c3d4.rbxl"})
	if !check.Open || check.Matched || check.Notice != "" {
		t.Fatalf("check=%+v, want Open=true only", check)
	}
}

func TestCheckOpenReportsMatchedWhenThisProjectsPlaceIsAlreadyOpen(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "mine", Name: "my-game-a1b2c3d4.rbxl"}}})
	check := p.CheckOpen(context.Background(), Place{FileName: "my-game-a1b2c3d4.rbxl"})
	if check.Open || !check.Matched || check.Notice != "" {
		t.Fatalf("check=%+v, want Matched=true only", check)
	}
}

func TestCheckOpenRefusesWhenOtherInstancesAreOpen(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "other", Name: "someone-elses-b2c3d4e5.rbxl"}}})
	check := p.CheckOpen(context.Background(), Place{FileName: "my-game-a1b2c3d4.rbxl"})
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
	check := p.CheckOpen(context.Background(), Place{FileName: "my-game-a1b2c3d4.rbxl"})
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
	check := p.CheckOpen(context.Background(), Place{FileName: "my-game-a1b2c3d4.rbxl"})
	if !check.Open {
		t.Fatalf("check=%+v, want Open=true when there is nothing to probe", check)
	}
}

func TestCheckOpenIgnoresAnEmptyPlaceName(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one", Name: "whatever.rbxl"}}})
	check := p.CheckOpen(context.Background(), Place{})
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
	p.attachWindow = 100 * time.Millisecond
	p.retryEvery = 10 * time.Millisecond
	grant := p.Provision(context.Background(), "run-host-taken", "workspace-write", Target{
		Place: Place{FileName: "my-game-a1b2c3d4.rbxl"},
		Open:  func(context.Context) error { t.Fatal("launched a duplicate Studio over a host-taken one"); return nil },
	})
	if grant.Studio {
		t.Fatal("granted access though the WS host is owned by another client")
	}
	if !strings.Contains(grant.Notice, "another MCP client") {
		t.Fatalf("notice=%q, want the host-taken explanation", grant.Notice)
	}
}

func TestCheckOpenRefusesToLaunchOverAStudioHiddenByAnotherClient(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	p.Running = func(context.Context) bool { return true }
	p.attachWindow = 100 * time.Millisecond
	p.retryEvery = 10 * time.Millisecond
	check := p.CheckOpen(context.Background(), Place{FileName: "my-game-a1b2c3d4.rbxl"})
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
	check := p.CheckOpen(context.Background(), Place{FileName: "my-game-a1b2c3d4.rbxl"})
	if !check.Open || check.Matched || check.Notice != "" {
		t.Fatalf("check=%+v, want Open=true only", check)
	}
}

// Matching is by place file name, which on Windows is case-insensitive.
func TestProvisionMatchesPlaceNamesCaseInsensitively(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "mine", Name: "My-Game-A1B2C3D4.rbxl"}}})
	grant := p.Provision(context.Background(), "run-case", "workspace-write",
		Target{Place: Place{FileName: "my-game-a1b2c3d4.rbxl"}})
	if !grant.Studio {
		t.Fatalf("case difference refused the project's own Studio: %q", grant.Notice)
	}
}

// A place opened from roblox.com carries no local file, so the launcher reports
// it by the display name it has on Roblox. Matching only on the built file name
// refused such an instance permanently: the operator had the right place open
// the whole time, and no amount of reopening could ever satisfy the check.
func TestProvisionMatchesACloudPlaceByItsDisplayName(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "cloud", Name: "Asmr RNG"}}})
	grant := p.Provision(context.Background(), "run-cloud", "workspace-write", Target{
		Place: Place{FileName: "asmr-rng-b27c0859.rbxl", CloudName: "Asmr RNG"},
	})
	if !grant.Studio {
		t.Fatalf("the project's own Team Create Studio was refused: %q", grant.Notice)
	}
}

// A project edited on roblox.com is still recognised by its built file, for the
// operator who opens the local build instead.
func TestProvisionStillMatchesTheBuiltFileForACloudProject(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "local", Name: "asmr-rng-b27c0859.rbxl"}}})
	grant := p.Provision(context.Background(), "run-cloud-local", "workspace-write", Target{
		Place: Place{FileName: "asmr-rng-b27c0859.rbxl", CloudName: "Asmr RNG"},
	})
	if !grant.Studio {
		t.Fatalf("the project's built place was refused: %q", grant.Notice)
	}
}

// The cloud name must not become a wildcard: another project's Studio is still
// another project's.
func TestProvisionRefusesAnUnrelatedStudioForACloudProject(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "other", Name: "someone-elses-b2c3d4e5.rbxl"}}})
	grant := p.Provision(context.Background(), "run-cloud-foreign", "workspace-write", Target{
		Place: Place{FileName: "asmr-rng-b27c0859.rbxl", CloudName: "Asmr RNG"},
	})
	if grant.Studio {
		t.Fatal("granted access to an unrelated Studio")
	}
	// The refusal has to send the operator to Roblox; telling someone working in
	// Team Create to let StudioForge open the place points them at a local build
	// their collaborators are not in.
	if !strings.Contains(grant.Notice, "roblox.com") {
		t.Fatalf("notice=%q, want it to name where the place actually lives", grant.Notice)
	}
	if strings.Contains(grant.Notice, "let StudioForge open it automatically") {
		t.Fatalf("notice=%q, want no advice to auto-open a local build", grant.Notice)
	}
}

// Auto-open builds the project's own file and launches that. For a project
// edited on roblox.com this would put a second, unrelated window in front of
// the operator instead of the place their collaborators are in.
func TestProvisionNeverAutoOpensACloudPlace(t *testing.T) {
	transport := &openingTransport{place: "asmr-rng-b27c0859.rbxl"}
	p := newProvisioner(t, transport)
	opened := 0
	grant := p.Provision(context.Background(), "run-cloud-autoopen", "workspace-write", Target{
		Place: Place{FileName: "asmr-rng-b27c0859.rbxl", CloudName: "Asmr RNG"},
		Open:  func(context.Context) error { opened++; transport.open(); return nil },
	})
	if opened != 0 {
		t.Fatalf("a roblox.com place cannot be opened from a local build, opened=%d", opened)
	}
	if grant.Studio {
		t.Fatal("granted access though no Studio holds this project's place")
	}
	if !strings.Contains(grant.Notice, "open it from Roblox") {
		t.Fatalf("notice=%q, want the way forward stated", grant.Notice)
	}
}

// An instance still mid-registration reports no name at all. A cloud name must
// not turn that into a match, or a run would be handed whichever Studio the
// launcher happened to be registering.
func TestProvisionDoesNotMatchAnUnnamedInstanceOnACloudName(t *testing.T) {
	place := Place{FileName: "asmr-rng-b27c0859.rbxl", CloudName: "Asmr RNG"}
	if place.matches("") {
		t.Fatal("an unnamed instance was taken to hold this project's place")
	}
}
