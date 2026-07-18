package models

import "time"

type Project struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	Fingerprint   string    `json:"fingerprint"`
	Description   string    `json:"description"`
	GroupName     string    `json:"groupName,omitempty"`
	Tags          []string  `json:"tags"`
	Pinned        bool      `json:"pinned"`
	Archived      bool      `json:"archived"`
	Mock          bool      `json:"mock"`
	BudgetLimit   float64   `json:"budgetLimit"`
	BudgetUsed    float64   `json:"budgetUsed"`
	RunningAgents int       `json:"runningAgents"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
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

// TokenUsage is the token accounting persisted with a run. It is embedded in
// Run so the counters stay flat in the run JSON the UI already reads, and it
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
}

type ChatThread struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ChatMessage struct {
	Role   string    `json:"role"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
	RunID  string    `json:"runId"`
	Status string    `json:"status,omitempty"`
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

type Decision struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"projectId"`
	RunID          string    `json:"runId,omitempty"`
	Title          string    `json:"title"`
	Reason         string    `json:"reason"`
	ProposedAction string    `json:"proposedAction"`
	Risk           string    `json:"risk"`
	Preview        string    `json:"preview"`
	Status         string    `json:"status"`
	Resolution     string    `json:"resolution,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
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
