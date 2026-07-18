package scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/mock"
	"github.com/10kkyvl/studioforge/internal/resources"
)

// recordingProvider captures what the scheduler actually asked the provider to
// run, which is the only place the MCP grant becomes observable.
type recordingProvider struct {
	inner   providers.Provider
	mu      sync.Mutex
	reqs    []providers.RunRequest
	resumes []providers.ResumeRequest
}

func (r *recordingProvider) Diagnose(ctx context.Context) providers.Diagnostics {
	return r.inner.Diagnose(ctx)
}
func (r *recordingProvider) Start(ctx context.Context, req providers.RunRequest) (providers.RunHandle, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	return r.inner.Start(ctx, req)
}
func (r *recordingProvider) Resume(ctx context.Context, req providers.ResumeRequest) (providers.RunHandle, error) {
	r.mu.Lock()
	r.resumes = append(r.resumes, req)
	r.mu.Unlock()
	return r.inner.Resume(ctx, req)
}
func (r *recordingProvider) Cancel(ctx context.Context, id string) error {
	return r.inner.Cancel(ctx, id)
}
func (r *recordingProvider) requests() []providers.RunRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]providers.RunRequest(nil), r.reqs...)
}

func newHarness(t *testing.T) (*Manager, *recordingProvider, *database.Store, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "mcp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := database.NewStore(db)
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub(store)
	t.Cleanup(hub.Close)
	leases := resources.NewManager(time.Second)
	t.Cleanup(leases.Close)
	inner := mock.New()
	inner.StepDelay = time.Millisecond
	provider := &recordingProvider{inner: inner}
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider})
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return manager, provider, store, ctx
}

func waitForRequest(t *testing.T, provider *recordingProvider) providers.RunRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if reqs := provider.requests(); len(reqs) > 0 {
			return reqs[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("provider was never started")
	return providers.RunRequest{}
}

func waitForResume(t *testing.T, provider *recordingProvider) providers.ResumeRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		provider.mu.Lock()
		n := len(provider.resumes)
		var got providers.ResumeRequest
		if n > 0 {
			got = provider.resumes[0]
		}
		provider.mu.Unlock()
		if n > 0 {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("provider was never resumed")
	return providers.ResumeRequest{}
}

// Before this hook existed the only production RunRequest never set
// MCPConfigPath, so --mcp-config was never passed and no agent could reach
// Studio.
func TestSchedulerPassesMCPGrantToProvider(t *testing.T) {
	manager, provider, _, ctx := newHarness(t)
	released := make(chan struct{})
	var gotJob string
	manager.SetMCPProvisioner(func(_ context.Context, j *Job) MCPGrant {
		gotJob = j.PermissionProfile
		return MCPGrant{
			ConfigPath:   "C:\\configs\\run.json",
			AllowedTools: []string{"mcp__Roblox_Studio__script_read"},
			Release:      func() { close(released) },
		}
	})
	if _, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", PermissionProfile: "read-only", WorkingDirectory: t.TempDir(), MaxBudget: 1}); err != nil {
		t.Fatal(err)
	}
	req := waitForRequest(t, provider)
	if req.MCPConfigPath != "C:\\configs\\run.json" {
		t.Errorf("MCPConfigPath not handed to the provider: %q", req.MCPConfigPath)
	}
	if len(req.AllowedTools) != 1 || req.AllowedTools[0] != "mcp__Roblox_Studio__script_read" {
		t.Errorf("AllowedTools not handed to the provider: %q", req.AllowedTools)
	}
	if gotJob != "read-only" {
		t.Errorf("provisioner should see the job's permission profile, got %q", gotJob)
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Error("Release was never called, so generated configs would accumulate")
	}
}

// A job that carries a resume session must reach the provider through Resume,
// not Start — this is what makes a follow-up chat message continue the session.
func TestSchedulerResumesWhenSessionProvided(t *testing.T) {
	manager, provider, _, ctx := newHarness(t)
	if _, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1, ResumeSessionID: "sess-123", Prompt: "continue"}); err != nil {
		t.Fatal(err)
	}
	got := waitForResume(t, provider)
	if got.SessionID != "sess-123" {
		t.Errorf("scheduler must resume the provided session, got %q", got.SessionID)
	}
	if len(provider.requests()) != 0 {
		t.Errorf("a resume job must not also Start: %v", provider.requests())
	}
}

// The PLAN/DO choice must reach the provider so it can pass --permission-mode plan.
func TestSchedulerPassesModeToProvider(t *testing.T) {
	manager, provider, _, ctx := newHarness(t)
	if _, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1, Prompt: "plan it", Mode: "plan"}); err != nil {
		t.Fatal(err)
	}
	req := waitForRequest(t, provider)
	if req.Mode != "plan" {
		t.Errorf("scheduler must hand the mode to the provider, got %q", req.Mode)
	}
}

// A failed run must emit a terminal status event, not only an error event, or a
// chat UI that waits for a status:completed/failed/cancelled would hang forever.
func TestFailedRunEmitsTerminalStatus(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1, Prompt: "boom", Scenario: "crash"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events, err := store.EventsAfter(ctx, 0, "demo-obby", run.ID, 500)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if e.Type != "status" {
				continue
			}
			if payload, ok := e.Payload.(map[string]any); ok && payload["status"] == "failed" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a failed run never emitted a status:failed event; the chat UI would hang on it")
}

func TestSchedulerRunsWithoutAGrant(t *testing.T) {
	manager, provider, _, ctx := newHarness(t)
	manager.SetMCPProvisioner(func(context.Context, *Job) MCPGrant { return MCPGrant{Notice: "withheld"} })
	if _, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", PermissionProfile: "read-only", WorkingDirectory: t.TempDir(), MaxBudget: 1}); err != nil {
		t.Fatal(err)
	}
	req := waitForRequest(t, provider)
	if req.MCPConfigPath != "" || len(req.AllowedTools) != 0 {
		t.Errorf("a withheld grant must leave the run without MCP: path=%q tools=%q", req.MCPConfigPath, req.AllowedTools)
	}
}

// Subagents must reach the provider so an orchestrator lead can delegate to
// the project's other enabled agents via the provider's native mechanism.
func TestSchedulerPassesSubagents(t *testing.T) {
	manager, provider, _, ctx := newHarness(t)
	if _, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1, Subagents: []providers.Subagent{{Name: "Gameplay Engineer", Description: "Gameplay Engineer", Prompt: "Build features."}}}); err != nil {
		t.Fatal(err)
	}
	req := waitForRequest(t, provider)
	if len(req.Subagents) != 1 {
		t.Fatalf("scheduler must hand subagents to the provider, got %+v", req.Subagents)
	}
	if req.Subagents[0].Name != "Gameplay Engineer" {
		t.Errorf("subagent name=%q want %q", req.Subagents[0].Name, "Gameplay Engineer")
	}
}

// Every existing caller constructs the Manager without a provisioner.
func TestSchedulerWithoutProvisionerStillRuns(t *testing.T) {
	manager, provider, _, ctx := newHarness(t)
	if _, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1}); err != nil {
		t.Fatal(err)
	}
	if req := waitForRequest(t, provider); req.MCPConfigPath != "" {
		t.Errorf("unset provisioner must not invent a config: %q", req.MCPConfigPath)
	}
}
