package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/memory"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/resources"
)

// questionFencePattern matches a complete studioforge-question fenced block:
// the info-string on its own line, a JSON body, and a closing fence alone on
// its own line. Because it requires the closing fence, a block that is still
// mid-stream (opening fence seen, closing fence not yet arrived) simply does
// not match, so a partial chunk can never be mistaken for a real question.
var questionFencePattern = regexp.MustCompile("(?s)```studioforge-question\r?\n(.*?)\r?\n```")

// questionOption and questionBlock mirror the two-field JSON contract a coding
// agent emits when it wants the user to pick between discrete options; see
// the fenced-block format documented alongside the scheduler run loop.
type questionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}
type questionBlock struct {
	Question string           `json:"question"`
	Options  []questionOption `json:"options"`
}

// detectQuestion looks for a studioforge-question fenced block in a fully
// buffered message's text. Malformed JSON inside the fence, or a fence
// missing a question/options, is treated as ordinary text: it returns
// ok=false rather than an error, so callers never treat it as a crash or a
// false transition.
func detectQuestion(text string) (questionBlock, bool) {
	match := questionFencePattern.FindStringSubmatch(text)
	if match == nil {
		return questionBlock{}, false
	}
	var block questionBlock
	if err := json.Unmarshal([]byte(match[1]), &block); err != nil {
		return questionBlock{}, false
	}
	if strings.TrimSpace(block.Question) == "" || len(block.Options) == 0 {
		return questionBlock{}, false
	}
	return block, true
}

// messageText pulls the human-readable text out of a provider message
// event's payload, whatever shape that provider uses: the mock provider and
// Codex's item events carry a flat "text" field (Codex nests it one level
// under "item"), and Claude's assistant messages carry it in
// message.content[].text. This mirrors database.agentEventText, which
// extracts the same text from a persisted event for the chat transcript.
func messageText(payload any) string {
	decoded, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	if text, ok := decoded["text"].(string); ok && text != "" {
		return text
	}
	if item, ok := decoded["item"].(map[string]any); ok {
		if text, ok := item["text"].(string); ok && text != "" {
			return text
		}
	}
	if message, ok := decoded["message"].(map[string]any); ok {
		if content, ok := message["content"].([]any); ok {
			var parts []string
			for _, entryAny := range content {
				entry, ok := entryAny.(map[string]any)
				if !ok || entry["type"] != "text" {
					continue
				}
				if text, ok := entry["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n")
			}
		}
	}
	if text, ok := decoded["message"].(string); ok && text != "" {
		return text
	}
	return ""
}

// isFullyBufferedMessage reports whether a "message" event's raw provider
// type represents a complete message rather than a streaming delta chunk.
// Claude's stream_event and Codex's item.started/item.updated carry a
// message as it is still being assembled; the mock provider's own
// "assistant.partial" steps do the same for its deterministic demo. Only a
// complete message can be checked for a question block — checking a partial
// one risks matching on a fence that has not fully arrived yet.
func isFullyBufferedMessage(rawType string) bool {
	switch rawType {
	case "stream_event", "item.started", "item.updated":
		return false
	}
	return !strings.HasSuffix(rawType, ".partial")
}

type RunStore interface {
	CreateRun(context.Context, models.Run, string) (models.Run, bool, error)
	Run(context.Context, string) (models.Run, error)
	UpdateRun(context.Context, string, string, string, string, string) error
	SetRunUsage(context.Context, string, string, float64, models.TokenUsage) error
	BudgetAllowed(context.Context, string, float64) (bool, float64, float64, error)
}
type Job struct {
	RunID, ProjectID, AgentID, TaskID, Provider, Model, Effort, PermissionProfile string
	WorkingDirectory, Prompt, SystemPrompt, Scenario                              string
	ThreadID, ResumeSessionID, Mode                                               string
	Resources                                                                     []string
	MaxBudget                                                                     float64
	IdempotencyKey                                                                string
	// Subagents are handed to an orchestrator lead so it can delegate to the
	// project's other enabled agents via the provider's native mechanism.
	Subagents []providers.Subagent
}

// MCPGrant is the MCP access a run receives. An empty ConfigPath means none;
// Notice, when set, explains why and is surfaced as a run event.
type MCPGrant struct {
	ConfigPath   string
	AllowedTools []string
	Notice       string
	Context      string
	Release      func()
}

// MCPProvisioner decides a job's MCP access. It is deliberately stated in
// MCP terms rather than Roblox terms so the scheduler stays provider-neutral;
// the Studio-specific implementation is supplied at app construction.
type MCPProvisioner func(ctx context.Context, j *Job) MCPGrant

// SetMCPProvisioner installs the hook that grants runs their MCP access.
func (m *Manager) SetMCPProvisioner(p MCPProvisioner) {
	m.mu.Lock()
	m.provision = p
	m.mu.Unlock()
}

func (m *Manager) SetMemory(store *memory.Store) {
	m.mu.Lock()
	m.memoryStore = store
	m.mu.Unlock()
}

func (m *Manager) Diagnose(ctx context.Context, provider string) (providers.Diagnostics, bool) {
	m.mu.Lock()
	adapter, ok := m.providers[provider]
	m.mu.Unlock()
	if !ok || adapter == nil {
		return providers.Diagnostics{}, false
	}
	return adapter.Diagnose(ctx), true
}

type Manager struct {
	ctx                                                           context.Context
	cancel                                                        context.CancelFunc
	store                                                         RunStore
	hub                                                           *events.Hub
	leases                                                        *resources.Manager
	providers                                                     map[string]providers.Provider
	globalLimit, perProjectLimit, perProviderLimit, perModelLimit int
	provision                                                     MCPProvisioner
	memoryStore                                                   *memory.Store
	mu                                                            sync.Mutex
	queue                                                         *fairQueue
	active                                                        map[string]*execution
	projectActive, providerActive, modelActive                    map[string]int
	wake                                                          chan struct{}
	done                                                          chan struct{}
	closed                                                        bool
}
type execution struct {
	cancel   context.CancelFunc
	paused   bool
	resume   chan struct{}
	provider providers.Provider
	handle   providers.RunHandle
	job      *Job
	// question is set once a fully-buffered assistant message in this run
	// carried a studioforge-question fenced block, so the run's final
	// transition lands on waiting_decision instead of completed.
	question bool
}

func New(parent context.Context, store RunStore, hub *events.Hub, leases *resources.Manager, adapters map[string]providers.Provider) *Manager {
	ctx, cancel := context.WithCancel(parent)
	m := &Manager{ctx: ctx, cancel: cancel, store: store, hub: hub, leases: leases, providers: adapters, globalLimit: 6, perProjectLimit: 2, perProviderLimit: 4, perModelLimit: 3, queue: newFairQueue(), active: map[string]*execution{}, projectActive: map[string]int{}, providerActive: map[string]int{}, modelActive: map[string]int{}, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go m.loop()
	return m
}
func (m *Manager) SetLimits(global, project, provider, model int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if global > 0 {
		m.globalLimit = global
	}
	if project > 0 {
		m.perProjectLimit = project
	}
	if provider > 0 {
		m.perProviderLimit = provider
	}
	if model > 0 {
		m.perModelLimit = model
	}
	m.signal()
}

func (m *Manager) Submit(ctx context.Context, j Job) (models.Run, bool, error) {
	adapter, ok := m.providers[j.Provider]
	if !ok || adapter == nil {
		return models.Run{}, false, fmt.Errorf("provider %q is not configured", j.Provider)
	}
	if j.RunID == "" {
		j.RunID = ""
	}
	run, created, err := m.store.CreateRun(ctx, models.Run{ID: j.RunID, ProjectID: j.ProjectID, AgentID: j.AgentID, TaskID: j.TaskID, Provider: j.Provider, ModelAlias: j.Model, Status: "queued", Phase: "queued", ThreadID: j.ThreadID, PromptSnapshot: j.Prompt}, j.IdempotencyKey)
	if err != nil || !created {
		return run, created, err
	}
	j.RunID = run.ID
	if len(j.Resources) == 0 {
		j.Resources = []string{"project:" + j.ProjectID + ":write"}
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return models.Run{}, false, errors.New("scheduler is closed")
	}
	m.queue.push(&j)
	m.mu.Unlock()
	m.signal()
	m.emit(run, j.AgentID, "status", "scheduler.queued", map[string]any{"status": "queued"})
	return run, true, nil
}

func (m *Manager) loop() {
	defer close(m.done)
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		}
		for {
			m.mu.Lock()
			job := m.queue.pop(m.canStartLocked)
			if job == nil {
				m.mu.Unlock()
				break
			}
			ctx, cancel := context.WithCancel(m.ctx)
			exec := &execution{cancel: cancel, resume: make(chan struct{}), provider: m.providers[job.Provider], job: job}
			m.active[job.RunID] = exec
			m.projectActive[job.ProjectID]++
			m.providerActive[job.Provider]++
			m.modelActive[job.Provider+":"+job.Model]++
			m.mu.Unlock()
			go m.run(ctx, exec)
		}
	}
}
func (m *Manager) canStartLocked(j *Job) bool {
	return len(m.active) < m.globalLimit && m.projectActive[j.ProjectID] < m.perProjectLimit && m.providerActive[j.Provider] < m.perProviderLimit && m.modelActive[j.Provider+":"+j.Model] < m.perModelLimit
}
func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) run(ctx context.Context, e *execution) {
	j := e.job
	defer m.finished(j)
	allowed, limit, used, err := m.store.BudgetAllowed(ctx, j.ProjectID, j.MaxBudget)
	if err != nil {
		m.fail(ctx, j, "budget check failed: "+err.Error())
		return
	}
	if !allowed {
		m.fail(ctx, j, fmt.Sprintf("budget ceiling reached (used %.2f of %.2f)", used, limit))
		return
	}
	m.transition(ctx, j, "queued", "waiting_resources", "resources", first(j.Resources), "")
	lease, err := m.leases.Acquire(ctx, j.RunID, j.Resources)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			m.transition(context.Background(), j, "waiting_resources", "cancelled", "cancelled", "", "cancelled before resource acquisition")
		} else {
			m.fail(context.Background(), j, err.Error())
		}
		return
	}
	defer lease.Release()
	m.transition(ctx, j, "waiting_resources", "starting", "provider_start", "", "")
	m.mu.Lock()
	provision := m.provision
	m.mu.Unlock()
	var grant MCPGrant
	if provision != nil {
		grant = provision(ctx, j)
		if grant.Release != nil {
			defer grant.Release()
		}
		if grant.Notice != "" {
			m.emit(models.Run{ID: j.RunID, ProjectID: j.ProjectID}, j.AgentID, "status", "scheduler.mcp", map[string]any{"message": grant.Notice})
		}
	}
	prompt := j.Prompt
	if grant.Context != "" {
		prompt = "Current Roblox Studio place state (do not re-list it, build on it):\n" + grant.Context + "\n\n" + prompt
	}
	req := providers.RunRequest{RunID: j.RunID, ProjectID: j.ProjectID, AgentID: j.AgentID, WorkingDirectory: j.WorkingDirectory, Prompt: prompt, SystemPrompt: j.SystemPrompt, Mode: j.Mode, Model: j.Model, Effort: j.Effort, PermissionProfile: j.PermissionProfile, MaxBudget: j.MaxBudget, Scenario: j.Scenario, MCPConfigPath: grant.ConfigPath, AllowedTools: grant.AllowedTools, Subagents: j.Subagents}
	var handle providers.RunHandle
	if j.ResumeSessionID != "" {
		handle, err = e.provider.Resume(ctx, providers.ResumeRequest{RunRequest: req, SessionID: j.ResumeSessionID})
	} else {
		handle, err = e.provider.Start(ctx, req)
	}
	if err != nil {
		m.fail(ctx, j, err.Error())
		return
	}
	m.mu.Lock()
	e.handle = handle
	m.mu.Unlock()
	m.transition(ctx, j, "starting", "running", "agent", "", "")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	eventsCh := handle.Events()
	for eventsCh != nil {
		select {
		case <-ctx.Done():
			handle.Cancel()
			result := handle.Wait()
			// A cancelled run still spent tokens and money before it was
			// stopped, so it is recorded like any other. The run context is
			// already dead, hence the background one for both writes.
			_ = m.store.SetRunUsage(context.Background(), j.RunID, result.SessionID, result.Cost, models.TokenUsage(result.Usage))
			m.transition(context.Background(), j, "cancelling", "cancelled", "cancelled", "", "")
			return
		case <-ticker.C:
			if err := lease.Heartbeat(); err != nil {
				// The lease is gone (most likely reaped by the lease manager, e.g.
				// ErrLeaseLost), so the mutual-exclusion guarantee it provided no
				// longer holds: another run could now acquire the same project
				// write lock while this one keeps executing. Stop it rather than
				// let it run on silently as if healthy, mirroring how the
				// ctx.Done() and pause-cancel branches below abort an in-flight
				// provider process before recording a terminal state.
				slog.Error("run lost its project lease during execution", "run_id", j.RunID, "project_id", j.ProjectID, "error", err)
				handle.Cancel()
				result := handle.Wait()
				_ = m.store.SetRunUsage(ctx, j.RunID, result.SessionID, result.Cost, models.TokenUsage(result.Usage))
				m.fail(ctx, j, "lost project lock during execution")
				return
			}
		case event, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				break
			}
			if err := m.waitIfPaused(ctx, e); err != nil {
				handle.Cancel()
				m.transition(context.Background(), j, "cancelling", "cancelled", "cancelled", "", "")
				return
			}
			m.emitEvent(ctx, e, event)
		}
	}
	result := handle.Wait()
	_ = m.store.SetRunUsage(ctx, j.RunID, result.SessionID, result.Cost, models.TokenUsage(result.Usage))
	if result.Err != nil {
		m.fail(ctx, j, result.Err.Error())
		return
	}
	m.mu.Lock()
	asksQuestion := e.question
	m.mu.Unlock()
	if asksQuestion {
		// A studioforge-question fenced block appeared during this turn: the
		// run stops here to let the user pick an option instead of reporting
		// a normal completion. waiting_decision is resumable exactly like
		// completed (see database.LatestThreadSession), so the next message —
		// whether a clicked option or free text — continues this session.
		m.transition(ctx, j, "running", "waiting_decision", "waiting_decision", "", "")
		return
	}
	m.mu.Lock()
	mem := m.memoryStore
	m.mu.Unlock()
	if mem != nil {
		entry := memory.Entry{ProjectID: j.ProjectID, RunID: j.RunID, AgentID: j.AgentID, Content: truncate(j.Prompt, 2000), Summary: truncate(firstLine(j.Prompt), 140), Source: "run"}
		if err := mem.Put(ctx, entry); err != nil {
			slog.Warn("failed to persist run memory", "run_id", j.RunID, "error", err)
		}
	}
	m.transition(ctx, j, "running", "completed", "verified", "", "")
}
func firstLine(s string) string {
	if idx := strings.IndexAny(s, "\r\n"); idx >= 0 {
		return s[:idx]
	}
	return s
}
func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}
func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
func (m *Manager) waitIfPaused(ctx context.Context, e *execution) error {
	for {
		m.mu.Lock()
		if !e.paused {
			m.mu.Unlock()
			return nil
		}
		resume := e.resume
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-resume:
		}
	}
}
func (m *Manager) finished(j *Job) {
	m.mu.Lock()
	delete(m.active, j.RunID)
	m.projectActive[j.ProjectID]--
	m.providerActive[j.Provider]--
	m.modelActive[j.Provider+":"+j.Model]--
	m.mu.Unlock()
	m.signal()
}
func (m *Manager) fail(ctx context.Context, j *Job, message string) {
	if err := m.store.UpdateRun(ctx, j.RunID, "failed", "failed", "", message); err != nil {
		// The event below still fires, so the stream will now claim the run
		// failed even though the DB row disagrees — log loudly so that
		// divergence leaves a trace instead of vanishing silently.
		slog.Error("failed to persist run transition", "run_id", j.RunID, "status", "failed", "error", err)
	}
	m.emit(models.Run{ID: j.RunID, ProjectID: j.ProjectID}, j.AgentID, "error", "scheduler.failed", map[string]any{"message": message})
	// Also surface a terminal status event. Consumers such as the chat UI key off
	// status events to learn a run has ended; a failure that only emitted an
	// error event would leave them waiting on the run forever.
	m.emit(models.Run{ID: j.RunID, ProjectID: j.ProjectID}, j.AgentID, "status", "scheduler.state", map[string]any{"status": "failed", "phase": "failed"})
}
func (m *Manager) transition(ctx context.Context, j *Job, from, to, phase, resource, message string) {
	if err := ValidateTransition(from, to); err != nil && from != "" {
		m.emit(models.Run{ID: j.RunID, ProjectID: j.ProjectID}, j.AgentID, "error", "scheduler.transition", map[string]any{"message": err.Error()})
		return
	}
	if err := m.store.UpdateRun(ctx, j.RunID, to, phase, resource, message); err != nil {
		// Same risk as in fail(): the status event fired below will report
		// this transition as having happened even though the write that was
		// supposed to record it failed, leaving the DB row and the event
		// stream silently out of sync unless this is logged.
		slog.Error("failed to persist run transition", "run_id", j.RunID, "status", to, "error", err)
	}
	m.emit(models.Run{ID: j.RunID, ProjectID: j.ProjectID}, j.AgentID, "status", "scheduler.state", map[string]any{"status": to, "phase": phase, "resource": resource})
}
func (m *Manager) emit(run models.Run, agent, eventType, raw string, payload any) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = m.hub.Publish(ctx, models.RunEvent{ProjectID: run.ProjectID, RunID: run.ID, AgentID: agent, Type: eventType, RawType: raw, Payload: payload, CreatedAt: time.Now().UTC()})
}
func (m *Manager) emitEvent(ctx context.Context, e *execution, event providers.Event) {
	j := e.job
	// Providers report tokens in their own shapes and on their own events
	// (Claude on assistant messages and the result, Codex on turn.completed),
	// so the totals are republished once in a normalized usage event and the
	// UI never has to know which provider produced them.
	if event.Usage != (providers.Usage{}) {
		_, _ = m.hub.Publish(ctx, models.RunEvent{ProjectID: j.ProjectID, RunID: j.RunID, AgentID: j.AgentID, Type: "usage", RawType: event.RawType, Payload: models.TokenUsage(event.Usage), CreatedAt: event.At})
		// A provider event that carries nothing but usage has now been fully
		// delivered; anything else still has its own payload to publish.
		if event.Type == "usage" {
			return
		}
	}
	// A fully-buffered assistant message may carry a studioforge-question
	// fenced block asking the user to choose between discrete options. When
	// it does, a "question" event is published alongside the normal message
	// event (so the transcript still shows what the agent said) and the run
	// is flagged so its final transition lands on waiting_decision instead
	// of completed. Streaming delta chunks are skipped: a question fence
	// that has not fully arrived yet must never be matched early.
	if event.Type == "message" && isFullyBufferedMessage(event.RawType) {
		if block, ok := detectQuestion(messageText(event.Payload)); ok {
			m.mu.Lock()
			e.question = true
			m.mu.Unlock()
			_, _ = m.hub.Publish(ctx, models.RunEvent{ProjectID: j.ProjectID, RunID: j.RunID, AgentID: j.AgentID, Type: "question", RawType: event.RawType, Payload: map[string]any{"question": block.Question, "options": block.Options}, CreatedAt: event.At})
		}
	}
	_, _ = m.hub.Publish(ctx, models.RunEvent{ProjectID: j.ProjectID, RunID: j.RunID, AgentID: j.AgentID, Type: event.Type, RawType: event.RawType, Payload: event.Payload, CreatedAt: event.At})
}

func (m *Manager) Pause(ctx context.Context, runID string) error {
	m.mu.Lock()
	e, ok := m.active[runID]
	if !ok {
		m.mu.Unlock()
		return errors.New("run is not active")
	}
	if e.paused {
		m.mu.Unlock()
		return nil
	}
	e.paused = true
	e.resume = make(chan struct{})
	m.mu.Unlock()
	return m.store.UpdateRun(ctx, runID, "paused", "paused", "", "")
}
func (m *Manager) Resume(ctx context.Context, runID string) error {
	m.mu.Lock()
	e, ok := m.active[runID]
	if !ok {
		m.mu.Unlock()
		return errors.New("run is not active")
	}
	if !e.paused {
		m.mu.Unlock()
		return nil
	}
	e.paused = false
	close(e.resume)
	m.mu.Unlock()
	return m.store.UpdateRun(ctx, runID, "running", "agent", "", "")
}
func (m *Manager) Cancel(ctx context.Context, runID string) error {
	m.mu.Lock()
	e, ok := m.active[runID]
	if ok {
		// Persist the intermediate state before signalling cancellation. If the
		// provider exits quickly, the run goroutine can otherwise write
		// "cancelled" first and this method would overwrite that terminal state
		// with "cancelling".
		if err := m.store.UpdateRun(ctx, runID, "cancelling", "cancelling", "", ""); err != nil {
			m.mu.Unlock()
			return err
		}
		e.cancel()
		if e.handle != nil {
			_ = e.handle.Cancel()
		}
		m.mu.Unlock()
		return nil
	}
	// A run that is still queued has no goroutine and no provider process, so
	// there is nothing to signal: take it out of the queue and record the
	// terminal state directly. queued -> cancelled is a legal transition, so it
	// needs no intermediate "cancelling" step, and removing it first means the
	// scheduler loop can never pop and start a run we just cancelled.
	job, queued := m.queue.remove(runID)
	m.mu.Unlock()
	if queued {
		m.transition(ctx, job, "queued", "cancelled", "cancelled", "", "cancelled while queued")
		return nil
	}
	return errors.New("run is not active")
}
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	for _, e := range m.active {
		e.cancel()
	}
	m.mu.Unlock()
	m.cancel()
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
