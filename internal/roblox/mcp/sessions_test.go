package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

// sessionsTransport tracks which instance set_active_studio last selected, and
// answers get_studio_state for whichever one that was — the same per-connection
// "active instance" model the real launcher uses, so ListSessions must select
// before it can read a given instance's own state.
type sessionsTransport struct {
	studioTransport
	states     map[string]string
	stateErr   map[string]error
	selectErr  map[string]error
	selections []string
}

func (s *sessionsTransport) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	switch name {
	case "set_active_studio":
		id, _ := args["studio_id"].(string)
		if err := s.selectErr[id]; err != nil {
			return nil, err
		}
		s.selections = append(s.selections, id)
		return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
	case "get_studio_state":
		id := ""
		if len(s.selections) > 0 {
			id = s.selections[len(s.selections)-1]
		}
		if err := s.stateErr[id]; err != nil {
			return nil, err
		}
		body, err := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": s.states[id]}}})
		if err != nil {
			return nil, err
		}
		return body, nil
	default:
		return s.studioTransport.Call(ctx, name, args)
	}
}

// Listing every open instance is the whole point of this view, so — unlike
// Provision and Validate — more than one open Studio must not be refused.
func TestListSessionsReportsEveryOpenInstanceWithoutRefusingAmbiguity(t *testing.T) {
	transport := &sessionsTransport{
		studioTransport: studioTransport{instances: []Instance{{ID: "one", Name: "Obby-a1b2c3d4.rbxl", Active: true}, {ID: "two", Name: "Tycoon-e5f6a7b8.rbxl"}}},
		states:          map[string]string{"one": "Studio Mode: Edit", "two": "Studio Mode: Play"},
	}
	p := newProvisioner(t, transport)
	sessions, err := p.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !sessions.Detected {
		t.Fatal("a reachable launcher must report Detected=true")
	}
	if len(sessions.Instances) != 2 {
		t.Fatalf("instances=%d, want 2 (both, not refused for being ambiguous)", len(sessions.Instances))
	}
	byID := map[string]Session{}
	for _, s := range sessions.Instances {
		byID[s.InstanceID] = s
	}
	if byID["one"].Name != "Obby-a1b2c3d4.rbxl" || byID["one"].PlayState != "edit" || !byID["one"].Active {
		t.Errorf("instance one=%+v", byID["one"])
	}
	if byID["two"].Name != "Tycoon-e5f6a7b8.rbxl" || byID["two"].PlayState != "play" {
		t.Errorf("instance two=%+v", byID["two"])
	}
}

func TestListSessionsWhenLauncherIsAbsent(t *testing.T) {
	p := newProvisioner(t, &sessionsTransport{})
	dir := t.TempDir()
	p.Override = func() string { return filepath.Join(dir, "definitely-missing-launcher") }
	sessions, err := p.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sessions.Detected {
		t.Error("an absent launcher must report Detected=false, not a silent empty list")
	}
	if len(sessions.Instances) != 0 {
		t.Errorf("instances=%v, want none", sessions.Instances)
	}
}

func TestListSessionsWithNoStudioOpen(t *testing.T) {
	p := newProvisioner(t, &sessionsTransport{studioTransport: studioTransport{instances: nil}})
	sessions, err := p.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !sessions.Detected {
		t.Error("a reachable launcher with nothing open must still report Detected=true")
	}
	if len(sessions.Instances) != 0 || sessions.Blocked {
		t.Errorf("sessions=%+v, want an empty, unblocked list", sessions)
	}
}

// An empty instance list from a launcher that connected fine is ambiguous by
// itself: it could mean Studio is closed, or that another MCP client already
// holds the WS host. Running distinguishes them, the same way Status does.
func TestListSessionsWhenAnotherClientHoldsStudio(t *testing.T) {
	p := newProvisioner(t, &sessionsTransport{studioTransport: studioTransport{instances: nil}})
	p.Running = func(context.Context) bool { return true }
	p.attachWindow = 1
	p.retryEvery = 1
	sessions, err := p.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !sessions.Detected || !sessions.Blocked {
		t.Errorf("sessions=%+v, want Detected and Blocked", sessions)
	}
}

// One instance's state failing to read must not take the rest of the listing
// down with it, or drop the failed instance from view either — an operator
// still needs to see that it is open, just with an unknown play state.
func TestListSessionsTreatsAPerInstanceStateFailureAsUnknownNotFatal(t *testing.T) {
	transport := &sessionsTransport{
		studioTransport: studioTransport{instances: []Instance{{ID: "one", Name: "A.rbxl"}, {ID: "two", Name: "B.rbxl"}}},
		states:          map[string]string{"two": "Studio Mode: Edit"},
		selectErr:       map[string]error{"one": errors.New("select failed")},
	}
	p := newProvisioner(t, transport)
	sessions, err := p.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions.Instances) != 2 {
		t.Fatalf("instances=%d, want 2 even though one's state could not be read", len(sessions.Instances))
	}
	byID := map[string]Session{}
	for _, s := range sessions.Instances {
		byID[s.InstanceID] = s
	}
	if byID["one"].PlayState != "" {
		t.Errorf("instance one playState=%q, want empty (unknown)", byID["one"].PlayState)
	}
	if byID["two"].PlayState != "edit" {
		t.Errorf("instance two playState=%q, want edit", byID["two"].PlayState)
	}
}
