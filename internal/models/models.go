package models

import "time"

type Project struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Path          string   `json:"path"`
	Fingerprint   string   `json:"fingerprint"`
	Description   string   `json:"description"`
	GroupName     string   `json:"groupName,omitempty"`
	Tags          []string `json:"tags"`
	Pinned        bool     `json:"pinned"`
	Archived      bool     `json:"archived"`
	Mock          bool     `json:"mock"`
	BudgetLimit   float64  `json:"budgetLimit"`
	BudgetUsed    float64  `json:"budgetUsed"`
	RunningAgents int      `json:"runningAgents"`
	// TokenUsage here is not one run's counters but the SUM of every run's
	// counters in the project, so the project card can answer "what has this
	// project spent" without the caller re-summing runs itself.
	TokenUsage
	// Sync is the project's live Rojo sync session, if any. It is never stored:
	// api.Server fills it in from the running rojo.Manager after loading the
	// project, because a `rojo serve` process is daemon-lifetime state that no
	// SQL query could answer.
	Sync      SyncStatus `json:"sync"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// SyncStatus is whether a project's files are being pushed live into an open
// Roblox Studio via `rojo serve`, and since when. It rides on the project
// payload instead of its own polling endpoint — contrast StudioStatus, which
// the chat badge polls — because it only ever changes in response to a
// project's own sync/unsync calls or the session dying on its own; nothing
// external moves it the way another MCP client can steal Studio's connection.
type SyncStatus struct {
	Active    bool      `json:"active"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"startedAt"`
	// RecentLogs are the session's most recent `rojo serve` log lines, oldest
	// first, so the project Overview can show what a live session is
	// currently doing without a dedicated polling endpoint of its own.
	RecentLogs []string `json:"recentLogs,omitempty"`
}

type Agent struct {
	ID           string  `json:"id"`
	ProjectID    string  `json:"projectId"`
	Name         string  `json:"name"`
	Role         string  `json:"role"`
	Provider     string  `json:"provider"`
	ModelAlias   string  `json:"modelAlias"`
	Effort       string  `json:"effort"`
	Enabled      bool    `json:"enabled"`
	Permission   string  `json:"permission"`
	Concurrency  int     `json:"concurrency"`
	Budget       float64 `json:"budget"`
	SystemPrompt string  `json:"systemPrompt"`
	// ValidateAfterRun opts this agent into the post-run Studio playtest
	// validation loop (Claude runs only, workspace-write permission or
	// above, and only when a run actually received a Studio MCP grant).
	// Off by default: the loop is opt-in, not automatic.
	ValidateAfterRun bool `json:"validateAfterRun"`
	// MaxCorrectionRuns bounds how many follow-up correction runs a failed
	// validation may schedule for this agent's runs, in one lineage.
	MaxCorrectionRuns int `json:"maxCorrectionRuns"`
	// StuckDetectionDisabled opts this agent OUT of the stuck-run escalation
	// (on by default globally): set for an agent that is expected by design to
	// run very long, so a naturally long session never gets flagged.
	StuckDetectionDisabled bool `json:"stuckDetectionDisabled"`
}

type Task struct {
	ID                 string   `json:"id"`
	ProjectID          string   `json:"projectId"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria string   `json:"acceptanceCriteria"`
	Priority           int      `json:"priority"`
	Status             string   `json:"status"`
	AssignedAgentID    string   `json:"assignedAgentId,omitempty"`
	Dependencies       []string `json:"dependencies"`
	BlockedReason      string   `json:"blockedReason,omitempty"`
}

// TokenUsage is the per-run token accounting, and also the shape used for its
// aggregates: Project and ChatThread embed the same four counters as a SUM
// over every run in the project or thread, respectively. Embedding in Run
// keeps the counters flat in the run JSON the UI already reads, and it
// mirrors providers.Usage field for field so the two convert directly.
type TokenUsage struct {
	InputTokens         int `json:"inputTokens"`
	OutputTokens        int `json:"outputTokens"`
	CacheReadTokens     int `json:"cacheReadTokens"`
	CacheCreationTokens int `json:"cacheCreationTokens"`
}

type Run struct {
	ID               string  `json:"id"`
	ProjectID        string  `json:"projectId"`
	AgentID          string  `json:"agentId"`
	TaskID           string  `json:"taskId,omitempty"`
	Provider         string  `json:"provider"`
	ModelAlias       string  `json:"modelAlias"`
	ProviderSession  string  `json:"providerSession,omitempty"`
	Status           string  `json:"status"`
	Phase            string  `json:"phase"`
	RequiredResource string  `json:"requiredResource,omitempty"`
	Error            string  `json:"error,omitempty"`
	Cost             float64 `json:"cost"`
	TokenUsage
	BaseCommit     string     `json:"baseCommit,omitempty"`
	ResultCommit   string     `json:"resultCommit,omitempty"`
	ThreadID       string     `json:"threadId,omitempty"`
	PromptSnapshot string     `json:"promptSnapshot,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	// Validation is the post-run Studio playtest outcome: none (never ran,
	// or opted out), passed, failed, inconclusive, corrected (a follow-up
	// correction run later passed), or correction_failed (corrections were
	// exhausted without a pass).
	Validation string `json:"validation"`
	// ValidationScreenshot is the reference/path the screen_capture tool
	// returned during the validation pass, if one ran.
	ValidationScreenshot string `json:"validationScreenshot,omitempty"`
	// ParentRunID is set on a correction run: the run whose failed
	// validation scheduled it.
	ParentRunID string `json:"parentRunId,omitempty"`
	// CorrectionDepth is 0 for an organic run, and the parent's depth+1 for
	// a correction run, so the loop can bound how many corrections one
	// original failure may chain.
	CorrectionDepth int `json:"correctionDepth"`
	// StuckEscalated is true when this run's own termination was the scheduler
	// stopping it and asking the operator to continue or stop, rather than the
	// agent completing, failing, or asking its own question.
	StuckEscalated bool `json:"stuckEscalated,omitempty"`
}

type ChatThread struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	// Same aggregate as Project.TokenUsage, scoped to this thread's runs
	// instead of the whole project, so the chat header can show what a
	// conversation has spent.
	TokenUsage
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ChatMessage struct {
	Role   string    `json:"role"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
	RunID  string    `json:"runId"`
	Status string    `json:"status,omitempty"`
	// RawType carries the scheduler's own event RawType through to a
	// persisted, reloaded message when this message came from a
	// scheduler-synthesized event (e.g. "scheduler.stuck") rather than the
	// agent's own text, so the frontend can tell the two apart identically on
	// a live SSE event and after a reload. Empty for an ordinary agent message
	// assembled from more than one event, or for a user message.
	RawType string `json:"rawType,omitempty"`
}

type RunEvent struct {
	ID        int64     `json:"id"`
	ProjectID string    `json:"projectId"`
	RunID     string    `json:"runId"`
	AgentID   string    `json:"agentId,omitempty"`
	Type      string    `json:"type"`
	RawType   string    `json:"rawType,omitempty"`
	Payload   any       `json:"payload"`
	CreatedAt time.Time `json:"createdAt"`
}

// Decision is an operator-approval gate: something the scheduler would
// otherwise have to silently decide on its own. Payload carries whatever a
// producer needs to act on approval (currently the JSON-encoded scheduler.Job
// of the correction run it is proposing); internal/models deliberately does
// not know its shape, so a Decision's kind decides how its own consumer
// interprets Payload.
type Decision struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"projectId"`
	RunID      string     `json:"runId"`
	Kind       string     `json:"kind"`
	Summary    string     `json:"summary"`
	Detail     string     `json:"detail,omitempty"`
	Payload    string     `json:"-"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
}

type Checkpoint struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	RunID      string    `json:"runId,omitempty"`
	CommitHash string    `json:"commitHash"`
	Branch     string    `json:"branch"`
	Label      string    `json:"label"`
	CreatedAt  time.Time `json:"createdAt"`
}

type StudioSession struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId,omitempty"`
	InstanceID string    `json:"instanceId"`
	Name       string    `json:"name"`
	PlaceID    string    `json:"placeId,omitempty"`
	GameID     string    `json:"gameId,omitempty"`
	Active     bool      `json:"active"`
	PlayState  string    `json:"playState"`
	Mock       bool      `json:"mock"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type Diagnostics struct {
	Version      string           `json:"version"`
	Commit       string           `json:"commit"`
	BuildDate    string           `json:"buildDate"`
	OS           string           `json:"os"`
	Arch         string           `json:"arch"`
	DataPath     string           `json:"dataPath"`
	Database     string           `json:"database"`
	WAL          bool             `json:"wal"`
	FTS5         bool             `json:"fts5"`
	SafeMode     bool             `json:"safeMode"`
	MockMode     bool             `json:"mockMode"`
	Dependencies map[string]Check `json:"dependencies"`
	Checks       []Check          `json:"checks"`
}

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message,omitempty"`
	Help    string `json:"help,omitempty"`
}
