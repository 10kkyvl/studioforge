package mock

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/10kkyvl/studioforge/internal/providers"
)

type Provider struct {
	mu        sync.Mutex
	runs      map[string]context.CancelFunc
	StepDelay time.Duration
}

func New() *Provider {
	return &Provider{runs: map[string]context.CancelFunc{}, StepDelay: 90 * time.Millisecond}
}
func (p *Provider) Diagnose(context.Context) providers.Diagnostics {
	return providers.Diagnostics{Available: true, Authenticated: true, Version: "mock-1.0", Path: "embedded", Capabilities: map[string]bool{"stream-json": true, "resume": true, "structured-output": true}, Message: "Embedded deterministic provider"}
}
func (p *Provider) Start(ctx context.Context, req providers.RunRequest) (providers.RunHandle, error) {
	return p.start(ctx, req, "")
}
func (p *Provider) Resume(ctx context.Context, req providers.ResumeRequest) (providers.RunHandle, error) {
	if req.SessionID == "" {
		return nil, errors.New("mock resume requires a session ID")
	}
	return p.start(ctx, req.RunRequest, req.SessionID)
}
func (p *Provider) start(parent context.Context, req providers.RunRequest, session string) (providers.RunHandle, error) {
	if req.RunID == "" {
		return nil, errors.New("run ID is required")
	}
	ctx, cancel := context.WithCancel(parent)
	if session == "" {
		session = "mock-session-" + req.RunID
	}
	h := &handle{events: make(chan providers.Event, 16), done: make(chan struct{}), cancel: cancel}
	p.mu.Lock()
	p.runs[req.RunID] = cancel
	p.mu.Unlock()
	go p.execute(ctx, req, session, h)
	return h, nil
}
func (p *Provider) execute(ctx context.Context, req providers.RunRequest, session string, h *handle) {
	defer close(h.events)
	defer close(h.done)
	defer func() { p.mu.Lock(); delete(p.runs, req.RunID); p.mu.Unlock() }()
	steps := []providers.Event{{Type: "status", RawType: "mock.start", Payload: map[string]any{"message": "Agent session started"}}, {Type: "message", RawType: "assistant.partial", Payload: map[string]any{"text": "Reading project constitution and task contract…"}}, {Type: "tool", RawType: "tool.use", Payload: map[string]any{"tool": "Read", "target": ".agent/constitution.yaml"}}, {Type: "message", RawType: "assistant.partial", Payload: map[string]any{"text": "Implementing the bounded milestone change…"}}, {Type: "artifact", RawType: "artifact.created", Payload: map[string]any{"kind": "handoff", "name": "task-handoff.json"}}, {Type: "usage", RawType: "result.usage", Payload: map[string]any{"inputTokens": 1200, "outputTokens": 680, "cost": 0.42}, Cost: 0.42}, {Type: "message", RawType: "assistant.final", Payload: map[string]any{"text": "Acceptance criteria verified in mock mode."}}}
	if req.Scenario == "hang" {
		<-ctx.Done()
		h.result = providers.Result{SessionID: session, Err: ctx.Err(), ExitCode: -1}
		return
	}
	for i, event := range steps {
		if req.Scenario == "crash" && i == 3 {
			h.result = providers.Result{SessionID: session, Err: errors.New("mock provider crashed"), ExitCode: 17}
			return
		}
		if req.Scenario == "rate_limit" && i == 2 {
			h.result = providers.Result{SessionID: session, Err: errors.New("mock rate limit"), ExitCode: 1}
			return
		}
		event.SessionID = session
		event.At = time.Now().UTC()
		select {
		case <-ctx.Done():
			h.result = providers.Result{SessionID: session, Err: ctx.Err(), ExitCode: -1}
			return
		case h.events <- event:
		}
		timer := time.NewTimer(p.StepDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			h.result = providers.Result{SessionID: session, Err: ctx.Err(), ExitCode: -1}
			return
		case <-timer.C:
		}
	}
	h.result = providers.Result{SessionID: session, Cost: 0.42, ExitCode: 0}
}
func (p *Provider) Cancel(_ context.Context, runID string) error {
	p.mu.Lock()
	cancel, ok := p.runs[runID]
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("mock run %s not found", runID)
	}
	cancel()
	return nil
}

type handle struct {
	events chan providers.Event
	done   chan struct{}
	cancel context.CancelFunc
	once   sync.Once
	result providers.Result
}

func (h *handle) Events() <-chan providers.Event { return h.events }
func (h *handle) Wait() providers.Result         { <-h.done; return h.result }
func (h *handle) Cancel() error                  { h.once.Do(h.cancel); return nil }
