package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type callRecordingTransport struct {
	studioTransport
	mu     sync.Mutex
	calls  []string
	closed bool
}

func (r *callRecordingTransport) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	r.mu.Lock()
	r.calls = append(r.calls, name)
	r.mu.Unlock()
	return r.studioTransport.Call(ctx, name, args)
}

func (r *callRecordingTransport) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return nil
}

func (r *callRecordingTransport) sawCall(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if c == name {
			return true
		}
	}
	return false
}

func TestProvisionLiveGrantsAccessAndSelectsTheMatchedInstance(t *testing.T) {
	transport := &callRecordingTransport{studioTransport: studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}}}
	p := newProvisioner(t, transport)
	grant := p.ProvisionLive(context.Background(), "workspace-write", Target{})
	if grant.Client == nil {
		t.Fatalf("expected a live client, got notice %q", grant.Notice)
	}
	if len(grant.AllowedTools) == 0 {
		t.Error("granting access without an allowlist leaves every tool call denied")
	}
	if !transport.sawCall("set_active_studio") {
		t.Error("ProvisionLive must select the matched instance on its own connection")
	}
	if grant.Release == nil {
		t.Fatal("a granted live client must offer a Release")
	}
	grant.Release()
	transport.mu.Lock()
	closed := transport.closed
	transport.mu.Unlock()
	if !closed {
		t.Error("Release must close the live transport")
	}
}

type selectStudioFailTransport struct {
	studioTransport
	mu     sync.Mutex
	closed bool
}

func (s *selectStudioFailTransport) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	if name == "set_active_studio" {
		return nil, errors.New("studio refused the pin")
	}
	return s.studioTransport.Call(ctx, name, args)
}

func (s *selectStudioFailTransport) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

func TestProvisionLiveWithholdsGrantWhenPinningTheStudioFails(t *testing.T) {
	transport := &selectStudioFailTransport{studioTransport: studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}}}
	p := newProvisioner(t, transport)
	grant := p.ProvisionLive(context.Background(), "workspace-write", Target{})
	if grant.Client != nil {
		grant.Release()
		t.Fatal("a failed Studio pin must not grant a live client")
	}
	if !strings.Contains(grant.Notice, "pinning the matched Studio instance failed") {
		t.Errorf("notice should explain the pin failure, got %q", grant.Notice)
	}
	transport.mu.Lock()
	closed := transport.closed
	transport.mu.Unlock()
	if !closed {
		t.Error("a withheld grant after a failed pin must still close the transport")
	}
}

func TestProvisionLiveBoundsAWedgedLauncherHandshake(t *testing.T) {
	p := newProvisioner(t, &studioTransport{})
	p.Timeout = 20 * time.Millisecond
	release := make(chan struct{})
	p.Dial = func(ctx context.Context, _ LaunchConfig) (Transport, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return &studioTransport{}, nil
	}
	start := time.Now()
	grant := p.ProvisionLive(context.Background(), "workspace-write", Target{})
	elapsed := time.Since(start)
	close(release)
	if grant.Client != nil {
		grant.Release()
		t.Fatal("a wedged launcher handshake must not grant a live client")
	}
	if grant.Notice == "" {
		t.Fatal("a wedged launcher handshake must explain why access was withheld")
	}
	if elapsed > 2*time.Second {
		t.Errorf("ProvisionLive took %s, want it bounded near the %s dial timeout", elapsed, p.Timeout)
	}
}

func TestProvisionLiveGrantsAccessWhenDialSucceedsWithinTheTimeout(t *testing.T) {
	transport := &callRecordingTransport{studioTransport: studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}}}
	p := newProvisioner(t, transport)
	p.Timeout = 20 * time.Millisecond
	grant := p.ProvisionLive(context.Background(), "workspace-write", Target{})
	if grant.Client == nil {
		t.Fatalf("expected a live client, got notice %q", grant.Notice)
	}
	if _, err := grant.Client.ListStudios(context.Background()); err != nil {
		t.Errorf("a granted client must stay usable past the dial timeout: %v", err)
	}
	grant.Release()
}

func TestProvisionLiveRefusesAmbiguousStudioSelection(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one"}, {ID: "two"}}})
	grant := p.ProvisionLive(context.Background(), "workspace-write", Target{})
	if grant.Client != nil {
		grant.Release()
		t.Fatal("two open Studios must not receive a live client")
	}
	if !strings.Contains(grant.Notice, "2 Studio instances") {
		t.Errorf("notice should say why access was withheld, got %q", grant.Notice)
	}
}

func TestProvisionLiveMissingLauncherIsNotAFailure(t *testing.T) {
	p := newProvisioner(t, &studioTransport{})
	p.Override = func() string { return filepath.Join(t.TempDir(), "absent") }
	grant := p.ProvisionLive(context.Background(), "workspace-write", Target{})
	if grant.Client != nil || grant.Notice != "" {
		t.Errorf("an absent launcher is an ordinary setup, got client=%v notice=%q", grant.Client, grant.Notice)
	}
}

func TestProvisionLiveExplainsAStudioHeldByAnotherClient(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: nil})
	p.Running = func(context.Context) bool { return true }
	grant := p.ProvisionLive(context.Background(), "workspace-write", Target{})
	if grant.Client != nil {
		grant.Release()
		t.Fatal("a Studio held by another client must not receive a live client")
	}
	if !strings.Contains(grant.Notice, "another MCP client") {
		t.Errorf("notice must name the cause the operator can act on, got %q", grant.Notice)
	}
}

func TestProvisionLiveRefusesUnknownProfile(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one"}}})
	grant := p.ProvisionLive(context.Background(), "", Target{})
	if grant.Client != nil {
		grant.Release()
		t.Fatal("an unrecognised profile must not receive a live client")
	}
	if !strings.Contains(grant.Notice, "grants no Studio tools") {
		t.Errorf("notice=%q", grant.Notice)
	}
}

func TestProvisionLiveOpensTheProjectsPlaceWhenNoneIsOpen(t *testing.T) {
	transport := &openingTransport{place: "my-game-a1b2c3d4.rbxl"}
	p := newProvisioner(t, transport)
	opened := false
	grant := p.ProvisionLive(context.Background(), "workspace-write", Target{
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
	if grant.Client == nil {
		t.Fatalf("no live client after opening: %q", grant.Notice)
	}
	grant.Release()
}
