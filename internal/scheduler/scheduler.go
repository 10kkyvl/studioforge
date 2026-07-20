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
	"github.com/10kkyvl/studioforge/internal/gitcheckpoint"
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
	UpdateRunIfStatus(ctx context.Context, id string, expectedStatuses []string, status, phase, resource, errText string) (bool, error)
	SetRunUsage(context.Context, string, string, float64, models.TokenUsage) error
	BudgetAllowed(context.Context, string, float64) (bool, float64, float64, error)
	SetRunValidation(ctx context.Context, id, validation, screenshot string) error
	// UpdateRunStuck writes a run's running->waiting_decision transition
	// together with the stuck_escalated bookkeeping that triggered it, in one
	// write (see database.Store.UpdateRunStuck).
	UpdateRunStuck(ctx context.Context, id, status, phase, resource, errText string) error
	CreateCheckpoint(ctx context.Context, checkpoint models.Checkpoint) error
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
	// ValidateAfterRun opts this job's agent into the post-run Studio
	// playtest validation loop. Off unless the agent explicitly turned it on.
	ValidateAfterRun bool
	// MaxCorrectionRuns bounds how many follow-up correction runs one failed
	// validation may chain, across the whole lineage.
	MaxCorrectionRuns int
	// ParentRunID and CorrectionDepth are set on a correction run: the run
	// whose failed validation scheduled it, and how deep into the correction
	// chain this run is (1 for the first correction attempt, and so on).
	ParentRunID     string
	CorrectionDepth int
	// StuckDetectionEnabled gates the whole stuck-detection safety net for
	// this job: the global stuck_detection_enabled setting AND'd with the
	// agent's own StuckDetectionDisabled opt-out, resolved once at Submit
	// time exactly like ValidateAfterRun.
	StuckDetectionEnabled bool
	// StuckIdleSeconds bounds the idle stuck check: how long a running job may
	// go without a single provider event before it is escalated.
	StuckIdleSeconds int
	// StuckRepetitionCap bounds the repeated-tool-cycle stuck check: how many
	// consecutive repeats of the same short tool-call sequence, with no file
	// edit and no new console/tool-result text, count as stuck.
	StuckRepetitionCap int
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

// ValidationOutcome mirrors mcp.ValidationOutcome at the scheduler boundary,
// the same way MCPGrant mirrors mcp.Grant, so this package stays provider-
// neutral and does not import internal/roblox/mcp.
type ValidationOutcome string

const (
	ValidationNone             ValidationOutcome = "none"
	ValidationPassed           ValidationOutcome = "passed"
	ValidationFailed           ValidationOutcome = "failed"
	ValidationInconclusive     ValidationOutcome = "inconclusive"
	ValidationCorrected        ValidationOutcome = "corrected"
	ValidationCorrectionFailed ValidationOutcome = "correction_failed"
)

// ValidationResult is one run's Studio playtest validation outcome.
type ValidationResult struct {
	Outcome    ValidationOutcome
	Console    string
	Errors     []string
	Screenshot string
	Notice     string
}

// MCPValidator runs a job's post-completion Studio playtest validation.
// Called only for a job that already qualifies (Claude, non-plan,
// workspace-write or above, opted in, and holding a real Studio grant) — the
// hook itself does not need to repeat those checks.
type MCPValidator func(ctx context.Context, j *Job) ValidationResult

// SetMCPValidator installs the hook that runs the post-run Studio playtest
// validation loop.
func (m *Manager) SetMCPValidator(v MCPValidator) {
	m.mu.Lock()
	m.validate = v
	m.mu.Unlock()
}

// isWorkspaceWriteOrAbove reports whether a permission profile includes the
// tools the validation loop's own start_stop_play call needs. Gating on this
// keeps the daemon's own Studio connection from acting with more reach than
// the run's own agent was granted.
func isWorkspaceWriteOrAbove(profile string) bool {
	return profile == "workspace-write" || profile == "danger-full-access"
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
	validate                                                      MCPValidator
	propose                                                       DecisionProposer
	memoryStore                                                   *memory.Store
	mu                                                            sync.Mutex
	queue                                                         *fairQueue
	active                                                        map[string]*execution
	projectActive, providerActive, modelActive                    map[string]int
	wake                                                          chan struct{}
	done                                                          chan struct{}
	closed                                                        bool
	// tick overrides the run loop's heartbeat/idle-check ticker interval;
	// zero means the 5-second default. Only tests shrink it.
	tick time.Duration
}
type execution struct {
	cancel   context.CancelFunc
	paused   bool
	resume   chan struct{}
	provider providers.Provider
	job      *Job
	// question is set once a fully-buffered assistant message in this run
	// carried a studioforge-question fenced block, so the run's final
	// transition lands on waiting_decision instead of completed.
	question bool
	// cancelling is set by Cancel under m.mu at the same moment it calls
	// cancel(), so Pause/Resume can detect a cancellation already in flight
	// and refuse to race their own status write against the run goroutine's
	// own cancelling/cancelled transitions.
	cancelling bool
	// lastEventAt is when the run loop last received a provider event, the
	// anchor the stuck-detection idle check measures against. Owned by the
	// run's own goroutine.
	//
	// The remaining fields are the stuck-detection repetition heuristic's
	// state, all owned exclusively by this run's own goroutine (never touched
	// by Pause/Resume/Cancel) and so read and written without m.mu:
	// toolCallsSinceEdit/obsCountAtToolCall are Claude tool-call names seen
	// since the last file-edit tool call (Edit/Write/MultiEdit resets both to
	// nil) paired with a snapshot of how many distinct tool-result
	// observations had been seen by that point; distinctObservations dedupes
	// those observations since the last edit, and recentObservations is the
	// same set capped and ordered for the escalation message.
	lastEventAt          time.Time
	toolCallsSinceEdit   []string
	obsCountAtToolCall   []int
	distinctObservations map[string]bool
	recentObservations   []string
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
	run, created, err := m.store.CreateRun(ctx, models.Run{ID: j.RunID, ProjectID: j.ProjectID, AgentID: j.AgentID, TaskID: j.TaskID, Provider: j.Provider, ModelAlias: j.Model, Status: "queued", Phase: "queued", ThreadID: j.ThreadID, PromptSnapshot: j.Prompt, ParentRunID: j.ParentRunID, CorrectionDepth: j.CorrectionDepth}, j.IdempotencyKey)
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
	m.transition(ctx, j, "starting", "running", "agent", "", "")
	e.lastEventAt = time.Now()
	interval := m.tick
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	eventsCh := handle.Events()
	for eventsCh != nil {
		select {
		case <-ctx.Done():
			m.transition(context.Background(), j, "running", "cancelling", "cancelling", "", "")
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
				// Same rationale as the ctx.Done() branch above: this run is
				// ending regardless of what the request-scoped ctx is doing, so
				// the terminal bookkeeping writes must not be silently dropped
				// by a ctx that could already be cancelled.
				_ = m.store.SetRunUsage(context.Background(), j.RunID, result.SessionID, result.Cost, models.TokenUsage(result.Usage))
				m.fail(context.Background(), j, "lost project lock during execution")
				return
			}
			if j.StuckDetectionEnabled {
				m.mu.Lock()
				skip := e.question || e.paused
				m.mu.Unlock()
				if skip {
					e.lastEventAt = time.Now()
				} else if reason, stuck := stuckIdleReason(time.Since(e.lastEventAt), j.StuckIdleSeconds); stuck {
					m.escalateStuck(ctx, j, e, handle, reason)
					return
				}
			}
		case event, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				break
			}
			if err := m.waitIfPaused(ctx, e); err != nil {
				m.transition(context.Background(), j, "paused", "cancelling", "cancelling", "", "")
				handle.Cancel()
				result := handle.Wait()
				_ = m.store.SetRunUsage(context.Background(), j.RunID, result.SessionID, result.Cost, models.TokenUsage(result.Usage))
				m.transition(context.Background(), j, "cancelling", "cancelled", "cancelled", "", "")
				return
			}
			e.lastEventAt = time.Now()
			m.emitEvent(ctx, e, event)
			// Stuck detection only evaluates once waitIfPaused has already
			// returned nil above (a run parked in a pause never reaches this
			// line at all until resumed), and never on top of the agent's own
			// natural question — that already has its own waiting_decision
			// path once this turn ends, and racing two escalation mechanisms
			// against the same run would be redundant at best.
			if j.StuckDetectionEnabled {
				m.trackStuckSignals(e, event)
				m.mu.Lock()
				alreadyQuestion := e.question
				m.mu.Unlock()
				if !alreadyQuestion {
					if reason, stuck := m.checkStuck(j, e); stuck {
						m.escalateStuck(ctx, j, e, handle, reason)
						return
					}
				}
			}
		}
	}
	result := handle.Wait()
	if ctx.Err() != nil {
		m.transition(context.Background(), j, "running", "cancelling", "cancelling", "", "")
		_ = m.store.SetRunUsage(context.Background(), j.RunID, result.SessionID, result.Cost, models.TokenUsage(result.Usage))
		m.transition(context.Background(), j, "cancelling", "cancelled", "cancelled", "", "")
		return
	}
	// From here on the provider process has already exited and the usage/cost
	// it spent is real regardless of what happens next, so every write below
	// that records what already happened uses context.Background() rather
	// than the request-scoped ctx: a Cancel arriving anywhere in this tail
	// (the execution stays in m.active, and so remains cancellable, until
	// this function actually returns) must never leave the DB row silently
	// stuck at its last-written status just because ctx died at an
	// inconvenient moment.
	_ = m.store.SetRunUsage(context.Background(), j.RunID, result.SessionID, result.Cost, models.TokenUsage(result.Usage))
	if result.Err != nil {
		m.fail(context.Background(), j, result.Err.Error())
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
		m.transition(context.Background(), j, "running", "waiting_decision", "waiting_decision", "", "")
		return
	}
	m.mu.Lock()
	mem := m.memoryStore
	m.mu.Unlock()
	if mem != nil {
		entry := memory.Entry{ProjectID: j.ProjectID, RunID: j.RunID, AgentID: j.AgentID, Content: truncate(j.Prompt, 2000), Summary: truncate(firstLine(j.Prompt), 140), Source: "run"}
		if err := mem.Put(context.Background(), entry); err != nil {
			slog.Warn("failed to persist run memory", "run_id", j.RunID, "error", err)
		}
	}
	// runValidation can run for a long time doing a real Studio playtest —
	// exactly the kind of long operation a Cancel can land during — so ctx is
	// checked both before starting it (skip it entirely once cancelled) and
	// after it returns (it may have been cancelled while validation itself
	// was running validate() already reacts to ctx.Done() promptly there,
	// but its own outcome is irrelevant once this run is being cancelled).
	if ctx.Err() != nil {
		m.transition(context.Background(), j, "running", "cancelling", "cancelling", "", "")
		m.transition(context.Background(), j, "cancelling", "cancelled", "cancelled", "", "")
		return
	}
	m.runValidation(ctx, j, grant, lease, result.SessionID)
	if ctx.Err() != nil {
		m.transition(context.Background(), j, "running", "cancelling", "cancelling", "", "")
		m.transition(context.Background(), j, "cancelling", "cancelled", "cancelled", "", "")
		return
	}
	m.transition(context.Background(), j, "running", "completed", "verified", "", "")
}

// validationHeartbeatFraction and its clamps size the validation phase's own
// lease-renewal ticker as a safe fraction of the configured lease TTL, so it
// still renews comfortably inside a short-TTL setup (e.g. tests) without
// hammering the lease manager under the normal 30-second production TTL.
const (
	validationHeartbeatFraction = 3
	minValidationHeartbeat      = 500 * time.Millisecond
	maxValidationHeartbeat      = 5 * time.Second
)

func validationHeartbeatInterval(ttl time.Duration) time.Duration {
	interval := ttl / validationHeartbeatFraction
	if interval > maxValidationHeartbeat {
		return maxValidationHeartbeat
	}
	if interval < minValidationHeartbeat {
		return minValidationHeartbeat
	}
	return interval
}

// runValidation runs the post-completion Studio playtest validation phase for
// a qualifying job (Claude, non-plan, workspace-write or above, opted in, and
// holding a real Studio grant for this run), persists its outcome, and
// schedules or resolves a correction as needed. Every other job is a no-op:
// the loop is fail-open on anything short of a genuine opt-in.
//
// The run loop's own 5-second lease heartbeat stops draining once the
// provider process exits, so without a heartbeat of its own here, a
// validation pass long enough to cross the lease TTL could let a second run
// steal this project's write lease while this one is still working.
func (m *Manager) runValidation(ctx context.Context, j *Job, grant MCPGrant, lease *resources.Handle, sessionID string) {
	m.mu.Lock()
	validate := m.validate
	m.mu.Unlock()
	if validate == nil || j.Provider != "claude" || j.Mode == "plan" || grant.ConfigPath == "" || !j.ValidateAfterRun || !isWorkspaceWriteOrAbove(j.PermissionProfile) {
		return
	}

	heartbeat := time.NewTicker(validationHeartbeatInterval(m.leases.TTL()))
	defer heartbeat.Stop()
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-heartbeat.C:
				if err := lease.Heartbeat(); err != nil {
					slog.Error("failed to renew project lease during validation", "run_id", j.RunID, "project_id", j.ProjectID, "error", err)
					return
				}
			}
		}
	}()
	validation := validate(ctx, j)
	close(stopHeartbeat)
	<-heartbeatDone

	outcome := validation.Outcome
	if outcome == "" {
		outcome = ValidationInconclusive
	}
	m.emitValidation(*j, validation, outcome)
	if err := m.store.SetRunValidation(context.Background(), j.RunID, string(outcome), validation.Screenshot); err != nil {
		slog.Error("failed to persist run validation", "run_id", j.RunID, "error", err)
	}

	switch outcome {
	case ValidationFailed:
		if j.CorrectionDepth < j.MaxCorrectionRuns {
			m.scheduleCorrection(ctx, j, sessionID, validation)
		} else {
			if j.ParentRunID != "" {
				if err := m.store.SetRunValidation(context.Background(), j.ParentRunID, string(ValidationCorrectionFailed), ""); err != nil {
					slog.Error("failed to mark parent run correction_failed", "parent_run_id", j.ParentRunID, "error", err)
				}
			}
			m.proposeCorrectionDecision(j, sessionID, validation)
		}
	case ValidationPassed:
		if j.ParentRunID != "" {
			if err := m.store.SetRunValidation(context.Background(), j.ParentRunID, string(ValidationCorrected), ""); err != nil {
				slog.Error("failed to mark parent run corrected", "parent_run_id", j.ParentRunID, "error", err)
			}
		}
	}
}

// emitValidation publishes a validation result as a normal run event so it
// survives in the Runs transcript and across a daemon restart, the same as
// any provider event.
func (m *Manager) emitValidation(j Job, validation ValidationResult, outcome ValidationOutcome) {
	payload := map[string]any{"outcome": string(outcome)}
	if len(validation.Errors) > 0 {
		payload["errors"] = validation.Errors
	}
	if validation.Screenshot != "" {
		payload["screenshot"] = validation.Screenshot
	}
	if validation.Notice != "" {
		payload["notice"] = validation.Notice
	}
	m.emit(models.Run{ID: j.RunID, ProjectID: j.ProjectID}, j.AgentID, "validation", "scheduler.validation", payload)
}

// scheduleCorrection submits a follow-up run for a failed validation,
// carrying the failure detail into its prompt and resuming the same CLI
// session so the agent has full context of what it just built. It goes
// through the normal Submit path — the same writer lease and budget ceiling
// apply as to any other run, and a budget refusal there is exactly "stop and
// surface the failure instead" (the correction run itself ends up failed,
// which TestCorrectionRunExceedingBudgetSurfacesAsAFailureNotASilentRetry
// covers). It takes its own Git checkpoint, mirroring the one internal/api
// takes before every other non-plan Claude run.
func (m *Manager) scheduleCorrection(ctx context.Context, j *Job, sessionID string, validation ValidationResult) {
	const checkpointLabel = "StudioForge checkpoint before correction run"
	var checkpointHash, checkpointBranch string
	if j.WorkingDirectory != "" {
		hash, branch, err := gitcheckpoint.Checkpoint(j.WorkingDirectory, checkpointLabel)
		if err != nil {
			slog.Warn("git checkpoint before correction run failed", "run_id", j.RunID, "project_id", j.ProjectID, "error", err)
		} else {
			checkpointHash, checkpointBranch = hash, branch
		}
	}
	correction := buildCorrectionJob(j, sessionID, validation)
	run, created, err := m.Submit(ctx, correction)
	if err != nil {
		slog.Error("failed to schedule correction run", "run_id", j.RunID, "project_id", j.ProjectID, "error", err)
		return
	}
	if created && checkpointHash != "" {
		checkpoint := models.Checkpoint{RunID: run.ID, ProjectID: j.ProjectID, CommitHash: checkpointHash, Branch: checkpointBranch, Label: checkpointLabel, CreatedAt: time.Now().UTC()}
		if err := m.store.CreateCheckpoint(ctx, checkpoint); err != nil {
			slog.Warn("persist checkpoint before correction run failed", "run_id", run.ID, "project_id", j.ProjectID, "error", err)
		}
	}
	m.emit(models.Run{ID: j.RunID, ProjectID: j.ProjectID}, j.AgentID, "status", "scheduler.correction_scheduled", map[string]any{"parentRunId": j.RunID})
}

// buildCorrectionJob is the follow-up run a failed validation would schedule
// for j: same project/agent/provider/permission, resuming j's own CLI session,
// with the console errors and screenshot reference folded into its prompt.
// Shared by scheduleCorrection (submitted immediately) and
// proposeCorrectionDecision (submitted only if an operator approves it).
func buildCorrectionJob(j *Job, sessionID string, validation ValidationResult) Job {
	return Job{
		ProjectID: j.ProjectID, AgentID: j.AgentID, TaskID: j.TaskID,
		Provider: j.Provider, Model: j.Model, Effort: j.Effort, PermissionProfile: j.PermissionProfile,
		WorkingDirectory: j.WorkingDirectory, SystemPrompt: j.SystemPrompt,
		Mode: j.Mode, ThreadID: j.ThreadID, ResumeSessionID: sessionID,
		MaxBudget: j.MaxBudget, Prompt: correctionPrompt(validation),
		ParentRunID: j.RunID, CorrectionDepth: j.CorrectionDepth + 1,
		MaxCorrectionRuns: j.MaxCorrectionRuns, ValidateAfterRun: j.ValidateAfterRun,
	}
}

// DecisionProposer records an operator-approval gate for a correction run the
// scheduler would otherwise have to silently give up on. Called only when a
// failed validation's correction count is exhausted (CorrectionDepth >=
// MaxCorrectionRuns) — every other case still schedules or resolves
// automatically, unaffected by whether one is installed. correction is the
// exact Job DecisionResolver's caller should submit if the operator approves.
type DecisionProposer func(ctx context.Context, runID, projectID, summary, detail string, correction Job)

// SetDecisionProposer installs the hook that records a pending Decision
// instead of silently giving up when a failed validation's correction count is
// exhausted. A nil (never installed) proposer leaves that case exactly as
// before: the lineage's direct parent is marked correction_failed and nothing
// further happens.
func (m *Manager) SetDecisionProposer(p DecisionProposer) {
	m.mu.Lock()
	m.propose = p
	m.mu.Unlock()
}

// proposeCorrectionDecision offers an operator the chance to override an
// exhausted correction budget for j, rather than the lineage silently staying
// at correction_failed forever. A no-op when no proposer is installed.
func (m *Manager) proposeCorrectionDecision(j *Job, sessionID string, validation ValidationResult) {
	m.mu.Lock()
	propose := m.propose
	m.mu.Unlock()
	if propose == nil {
		return
	}
	correction := buildCorrectionJob(j, sessionID, validation)
	summary := fmt.Sprintf("Correction run proposed for run %s: the automatic correction limit (%d) was reached", j.RunID, j.MaxCorrectionRuns)
	propose(context.Background(), j.RunID, j.ProjectID, summary, strings.Join(validation.Errors, "\n"), correction)
}

// correctionPrompt folds a failed validation's console errors and screenshot
// reference into the instruction a correction run receives.
func correctionPrompt(validation ValidationResult) string {
	var b strings.Builder
	b.WriteString("An automated Studio playtest ran after your last change and found a problem. Fix it, then report back.\n")
	if len(validation.Errors) > 0 {
		b.WriteString("\nConsole errors observed during Play mode:\n")
		for _, line := range validation.Errors {
			b.WriteString("- " + line + "\n")
		}
	}
	if validation.Screenshot != "" {
		b.WriteString("\nA screenshot was captured during the playtest: " + validation.Screenshot + "\n")
	}
	return b.String()
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
	if e.cancelling {
		m.mu.Unlock()
		return errors.New("run is being cancelled")
	}
	if e.paused {
		m.mu.Unlock()
		return nil
	}
	e.paused = true
	myResume := make(chan struct{})
	e.resume = myResume
	m.mu.Unlock()
	applied, err := m.store.UpdateRunIfStatus(ctx, runID, []string{"running"}, "paused", "paused", "", "")
	if err != nil {
		return err
	}
	if !applied {
		m.mu.Lock()
		if e.paused && e.resume == myResume {
			e.paused = false
			close(e.resume)
		}
		m.mu.Unlock()
		return errors.New("run status changed concurrently; refusing to pause")
	}
	return nil
}
func (m *Manager) Resume(ctx context.Context, runID string) error {
	m.mu.Lock()
	e, ok := m.active[runID]
	if !ok {
		m.mu.Unlock()
		return errors.New("run is not active")
	}
	if e.cancelling {
		m.mu.Unlock()
		return errors.New("run is being cancelled")
	}
	if !e.paused {
		m.mu.Unlock()
		return nil
	}
	e.paused = false
	myResume := e.resume
	close(myResume)
	m.mu.Unlock()
	applied, err := m.store.UpdateRunIfStatus(ctx, runID, []string{"paused"}, "running", "agent", "", "")
	if err != nil {
		return err
	}
	if !applied {
		m.mu.Lock()
		if !e.paused && e.resume == myResume {
			e.paused = true
			e.resume = make(chan struct{})
		}
		m.mu.Unlock()
		return errors.New("run status changed concurrently; refusing to resume")
	}
	return nil
}
func (m *Manager) Cancel(ctx context.Context, runID string) error {
	m.mu.Lock()
	e, ok := m.active[runID]
	if ok {
		e.cancelling = true
		e.cancel()
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
	// Not active and not queued: the one remaining case where a run is still
	// live from the operator's point of view but has no goroutine of its own
	// is waiting_decision — the run's own goroutine already fully exited (the
	// same way it does for the agent's own natural question) once it parked
	// there, so there is nothing to signal, exactly like the queued case
	// above, just against a different starting status and a two-step
	// transition (waiting_decision itself is not a legal direct predecessor
	// of cancelled; cancelling is the required intermediate step).
	run, err := m.store.Run(ctx, runID)
	if err != nil || run.Status != "waiting_decision" {
		return errors.New("run is not active")
	}
	pending := &Job{RunID: run.ID, ProjectID: run.ProjectID, AgentID: run.AgentID}
	m.transition(context.Background(), pending, "waiting_decision", "cancelling", "cancelling", "", "")
	m.transition(context.Background(), pending, "cancelling", "cancelled", "cancelled", "", "cancelled while waiting on a decision")
	return nil
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
