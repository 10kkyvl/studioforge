package mcp

import (
	"context"
	"errors"
	"strings"
)

// Session is one open Studio instance as reported by the launcher, meant for
// display in the Studio Sessions view.
type Session struct {
	InstanceID string
	Name       string
	// Active is the launcher's own notion of which instance is currently
	// focused, reported directly by list_roblox_studios — distinct from
	// PlayState, which is this instance's Edit/Play mode.
	Active bool
	// PlayState is "edit" or "play" when it could be determined from
	// get_studio_state, and empty when it could not (the state fetch for this
	// one instance failed, or returned text this couldn't classify).
	PlayState string
}

// Sessions is the result of one live discovery pass.
type Sessions struct {
	// Detected is false only when no Studio MCP launcher could be found at
	// all, so a caller can tell "nothing is open" from "there is nothing to
	// ask" and show a distinct "Studio MCP not detected" state instead of an
	// empty list.
	Detected bool
	// Blocked means Studio is running but another MCP client already holds
	// its connection, the same ambiguity Status reports.
	Blocked   bool
	Instances []Session
}

// ListSessions discovers every open Studio instance for display. Unlike
// Provision and Validate, it deliberately does not refuse on more than one
// open instance — those two are granting exclusive tool access, where
// ambiguity must fail closed; this is a read-only listing, where showing every
// open instance is the entire point. It never fails a caller: an absent or
// unreachable launcher yields an empty, explained result rather than an error.
func (p *Provisioner) ListSessions(ctx context.Context) (Sessions, error) {
	override := ""
	if p.Override != nil {
		override = p.Override()
	}
	launch, err := DetectLauncher(override)
	if err != nil {
		return Sessions{}, nil
	}
	instances, _, err := p.probe(ctx, launch)
	if errors.Is(err, errWSHostUnreachable) {
		return Sessions{Detected: true, Blocked: true}, nil
	}
	if err != nil {
		return Sessions{Detected: true}, err
	}
	if len(instances) == 0 {
		return Sessions{Detected: true, Blocked: p.blocked(ctx)}, nil
	}

	transport, err := p.dial(ctx, launch)
	if err != nil {
		return Sessions{Detected: true}, err
	}
	client := NewClient(transport)
	defer func() { _ = client.Close() }()

	out := make([]Session, 0, len(instances))
	for _, instance := range instances {
		out = append(out, Session{InstanceID: instance.ID, Name: instance.Name, Active: instance.Active, PlayState: p.instancePlayState(ctx, client, instance.ID)})
	}
	return Sessions{Detected: true, Instances: out}, nil
}

// instancePlayState selects one instance on the daemon's own probe connection
// and reads its state. A failure here — the select call itself, an
// unreadable response, or text this cannot classify — reports an unknown
// state for that one instance rather than dropping it from the list or
// failing the whole discovery pass.
func (p *Provisioner) instancePlayState(ctx context.Context, client *Client, instanceID string) string {
	if err := client.SelectStudio(ctx, instanceID); err != nil {
		return ""
	}
	raw, err := client.Call(ctx, "get_studio_state", nil)
	if err != nil {
		return ""
	}
	return parsePlayState(studioStateText(raw))
}

// parsePlayState reads Studio's own state text loosely: it is prose meant for
// an agent to read, not a documented enum, so this looks for either word
// rather than parsing a specific shape. Text saying neither is an unknown
// state, not a guess.
func parsePlayState(state string) string {
	lower := strings.ToLower(state)
	switch {
	case strings.Contains(lower, "play"):
		return "play"
	case strings.Contains(lower, "edit"):
		return "edit"
	default:
		return ""
	}
}
