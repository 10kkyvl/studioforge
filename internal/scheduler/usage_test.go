package scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/resources"
)

// A finished run is the only place the operator can see what it spent, so the
// token totals have to reach the row alongside the session and the cost.
func TestCompletedRunPersistsTokens(t *testing.T) {
	manager, _, store, ctx := newHarness(t)
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "completed", 5*time.Second)
	got, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := models.TokenUsage{InputTokens: 1200, OutputTokens: 680, CacheReadTokens: 4400}
	if got.TokenUsage != want {
		t.Errorf("persisted tokens=%+v want %+v", got.TokenUsage, want)
	}
	if got.ProviderSession == "" || got.Cost == 0 {
		t.Errorf("tokens must not displace the session and cost, got session=%q cost=%v", got.ProviderSession, got.Cost)
	}
	// Providers report tokens in their own shapes; exactly one normalized usage
	// event must reach the UI, or the live counter reads the wrong keys.
	usage := usageEvents(t, store, "demo-obby", run.ID)
	if len(usage) != 1 {
		t.Fatalf("usage events=%d want 1: %+v", len(usage), usage)
	}
	if usage[0]["outputTokens"] != float64(want.OutputTokens) || usage[0]["cacheReadTokens"] != float64(want.CacheReadTokens) {
		t.Errorf("usage payload=%+v want normalized token keys", usage[0])
	}
}

// Stopping a run does not refund it. The cancel path used to drop the provider
// result on the floor, so a cancelled run reported zero tokens forever.
func TestCancelledRunPersistsTokens(t *testing.T) {
	spent := providers.Usage{InputTokens: 800, OutputTokens: 120, CacheReadTokens: 6000}
	manager, store, ctx := newUsageHarness(t, &usageProvider{usage: spent})
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: t.TempDir(), MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "running", 5*time.Second)
	if err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "cancelled", 5*time.Second)
	got, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TokenUsage != models.TokenUsage(spent) {
		t.Errorf("cancelled run tokens=%+v want %+v", got.TokenUsage, spent)
	}
	if got.ProviderSession == "" {
		t.Error("a cancelled run must still record the session it spent them in")
	}
	// SetRunUsage writes usage_records in the same call, so a cancelled run's
	// cost must reach the budget gate too, not just the runs row.
	_, _, used, err := store.BudgetAllowed(ctx, "demo-obby", 0)
	if err != nil {
		t.Fatal(err)
	}
	if used <= 0 {
		t.Errorf("a cancelled run's cost must count toward the project's budget, used=%v", used)
	}
}

func usageEvents(t *testing.T, store *database.Store, projectID, runID string) []map[string]any {
	t.Helper()
	list, err := store.EventsAfter(context.Background(), 0, projectID, runID, 500)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, event := range list {
		payload, ok := event.Payload.(map[string]any)
		if event.Type == "usage" && ok {
			out = append(out, payload)
		}
	}
	return out
}

func newUsageHarness(t *testing.T, provider providers.Provider) (*Manager, *database.Store, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "usage.db"))
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
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider})
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return manager, store, ctx
}

// usageProvider reports its tokens and then runs until it is stopped, which is
// the shape the cancel path has to survive: the totals exist only in the result
// the scheduler collects after cancelling.
type usageProvider struct{ usage providers.Usage }

func (p *usageProvider) Diagnose(context.Context) providers.Diagnostics {
	return providers.Diagnostics{Available: true, Authenticated: true}
}
func (p *usageProvider) Start(ctx context.Context, _ providers.RunRequest) (providers.RunHandle, error) {
	h := &usageHandle{events: make(chan providers.Event, 1), done: make(chan struct{}), stop: make(chan struct{}), usage: p.usage}
	go h.stream(ctx)
	return h, nil
}
func (p *usageProvider) Resume(ctx context.Context, req providers.ResumeRequest) (providers.RunHandle, error) {
	return p.Start(ctx, req.RunRequest)
}
func (p *usageProvider) Cancel(context.Context, string) error { return nil }

type usageHandle struct {
	events chan providers.Event
	done   chan struct{}
	stop   chan struct{}
	once   sync.Once
	usage  providers.Usage
	result providers.Result
}

func (h *usageHandle) stream(ctx context.Context) {
	h.events <- providers.Event{Type: "usage", RawType: "test.usage", Usage: h.usage, At: time.Now().UTC()}
	select {
	case <-ctx.Done():
	case <-h.stop:
	}
	h.result = providers.Result{SessionID: "sess-cancelled", Cost: 0.5, Usage: h.usage, ExitCode: -1}
	close(h.events)
	close(h.done)
}
func (h *usageHandle) Events() <-chan providers.Event { return h.events }
func (h *usageHandle) Wait() providers.Result         { <-h.done; return h.result }
func (h *usageHandle) Cancel() error                  { h.once.Do(func() { close(h.stop) }); return nil }
