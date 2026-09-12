package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestProvisionBindsShimJournalToRun(t *testing.T) {
	p := newProvisioner(t, &studioTransport{instances: []Instance{{ID: "one", Name: "Place.rbxl"}}})
	p.JournalPath = "/private/data/studioforge.db"
	grant := p.Provision(context.Background(), "run-unique", "workspace-write", Target{})
	if grant.ConfigPath == "" {
		t.Fatal(grant.Notice)
	}
	defer grant.Release()
	body, err := os.ReadFile(grant.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(config.MCPServers[ServerName].Args, " ")
	if !strings.Contains(args, "--journal-db /private/data/studioforge.db --run-id run-unique") {
		t.Fatal(args)
	}
	p.Exe = func() (string, error) { return "", errors.New("not found") }
	if g := p.Provision(context.Background(), "run-2", "workspace-write", Target{}); g.ConfigPath != "" || !strings.Contains(g.Notice, "journal") {
		t.Fatalf("unlogged fallback granted: %+v", g)
	}
}

type journalSpy struct {
	starts                int
	statuses              []string
	failStart, failFinish bool
}

func (s *journalSpy) Start(ctx context.Context, _ string, _ map[string]any) (string, error) {
	if s.failStart {
		return "", errors.New("disk full")
	}
	s.starts++
	return "call", ctx.Err()
}
func (s *journalSpy) Finish(ctx context.Context, _ string, status string) error {
	s.statuses = append(s.statuses, status)
	if s.failFinish {
		return errors.New("disk full")
	}
	return ctx.Err()
}

type journalFake struct {
	calls  int
	raw    json.RawMessage
	err    error
	cancel context.CancelFunc
}

func (t *journalFake) ListTools(context.Context) ([]Tool, error) { return nil, nil }
func (t *journalFake) Close() error                              { return nil }
func (t *journalFake) Call(context.Context, string, map[string]any) (json.RawMessage, error) {
	t.calls++
	if t.cancel != nil {
		t.cancel()
	}
	return t.raw, t.err
}

func TestJournalDispatchAndFailureOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, tool, status            string
		raw                           string
		callErr                       error
		startFail, finishFail, cancel bool
		dispatched                    bool
	}{
		{name: "read only", tool: "script_read", raw: `{}`, dispatched: true},
		{name: "success", tool: "multi_edit", raw: `{"content":[]}`, status: "succeeded", dispatched: true},
		{name: "tool error", tool: "insert_asset", raw: `{"isError":true}`, status: "error", dispatched: true},
		{name: "connection lost", tool: "execute_luau", callErr: errors.New("lost"), status: "unknown", dispatched: true},
		{name: "malformed result", tool: "execute_luau", raw: `bad`, status: "unknown", dispatched: true},
		{name: "null result", tool: "execute_luau", raw: `null`, status: "unknown", dispatched: true},
		{name: "cancelled", tool: "execute_luau", raw: `{}`, status: "succeeded", cancel: true, dispatched: true},
		{name: "storage unavailable", tool: "execute_luau", startFail: true},
		{name: "outcome storage unavailable", tool: "execute_luau", raw: `{}`, status: "succeeded", finishFail: true, dispatched: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			spy := &journalSpy{failStart: tc.startFail, failFinish: tc.finishFail}
			transport := &journalFake{raw: json.RawMessage(tc.raw), err: tc.callErr}
			if tc.cancel {
				transport.cancel = cancel
			}
			client := NewClient(transport)
			client.SetRecorder(spy)
			_, err := client.Call(ctx, tc.tool, nil)
			if (transport.calls > 0) != tc.dispatched {
				t.Fatalf("calls=%d", transport.calls)
			}
			if tc.status != "" && (len(spy.statuses) != 1 || spy.statuses[0] != tc.status) {
				t.Fatalf("statuses=%v", spy.statuses)
			}
			if tc.tool == "script_read" && spy.starts != 0 {
				t.Fatal("read-only call recorded")
			}
			if (tc.startFail || tc.finishFail || tc.callErr != nil) && err == nil {
				t.Fatal("expected error")
			}
			if tc.cancel && err != nil {
				t.Fatalf("outcome lost on cancellation: %v", err)
			}
		})
	}
}

func TestShimJournalsMutationsOnly(t *testing.T) {
	spy := &journalSpy{}
	transport := &journalFake{raw: json.RawMessage(`{"content":[]}`)}
	responses := serveShim(t, ShimOptions{Dial: dialFake(transport), Recorder: spy},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"script_read","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"multi_edit","arguments":{"path":"ServerScriptService.Main"}}}`)
	if len(responses) != 2 || responses[1].Error != nil || spy.starts != 1 || len(spy.statuses) != 1 || spy.statuses[0] != "succeeded" {
		t.Fatalf("responses=%+v journal=%+v", responses, spy)
	}
}
