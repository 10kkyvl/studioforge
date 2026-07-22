package providers

import (
	"context"
	"time"
)

type Diagnostics struct {
	Available     bool            `json:"available"`
	Authenticated bool            `json:"authenticated"`
	Version       string          `json:"version"`
	Path          string          `json:"path"`
	Capabilities  map[string]bool `json:"capabilities"`
	Message       string          `json:"message"`
}
type RunRequest struct {
	RunID, ProjectID, AgentID, ThreadID, WorkingDirectory, Prompt, SystemPrompt, Mode, Model, Effort, PermissionProfile string
	MaxTurns                                                                                                            int
	MaxBudget                                                                                                           float64
	AllowUnverifiedModel                                                                                                bool
	MCPConfigPath                                                                                                       string
	// AllowedTools names the tools the run may call without an interactive
	// approval, which non-interactive runs cannot answer.
	AllowedTools []string
	// Attachments are project-relative paths to images the user attached to
	// this turn's prompt, handed to vision-capable providers as data alongside
	// the text prompt.
	Attachments []string
	Environment []string
	Scenario    string
	// Subagents are the project's other enabled agents, handed to an
	// orchestrator lead so it can delegate via the provider's native
	// subagent mechanism (e.g. Claude's --agents).
	Subagents []Subagent
}

// Subagent describes one delegate an orchestrator lead can hand work to.
type Subagent struct {
	Name        string
	Description string
	Prompt      string
}
type ResumeRequest struct {
	RunRequest
	SessionID string
}

// Usage is the token accounting a provider reports for a run. The cache
// counters are Claude-only; providers that do not report them leave them zero.
// They are kept apart from InputTokens because Claude counts cache hits
// outside it, so collapsing them would understate what a run actually read.
type Usage struct {
	InputTokens         int `json:"inputTokens"`
	OutputTokens        int `json:"outputTokens"`
	CacheReadTokens     int `json:"cacheReadTokens"`
	CacheCreationTokens int `json:"cacheCreationTokens"`
}

// Add folds one report into a running total.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		InputTokens:         u.InputTokens + other.InputTokens,
		OutputTokens:        u.OutputTokens + other.OutputTokens,
		CacheReadTokens:     u.CacheReadTokens + other.CacheReadTokens,
		CacheCreationTokens: u.CacheCreationTokens + other.CacheCreationTokens,
	}
}

type Event struct {
	Type      string  `json:"type"`
	RawType   string  `json:"rawType"`
	Payload   any     `json:"payload"`
	SessionID string  `json:"sessionId,omitempty"`
	Cost      float64 `json:"cost,omitempty"`
	// Usage is the run's cumulative total at this point in the stream rather
	// than the delta the provider reported, so a consumer can render the most
	// recent event it saw without repeating each provider's accumulation rules.
	Usage Usage     `json:"usage"`
	Error string    `json:"error,omitempty"`
	At    time.Time `json:"at"`
}
type Result struct {
	SessionID string
	Cost      float64
	Usage     Usage
	ExitCode  int
	Err       error
}
type RunHandle interface {
	Events() <-chan Event
	Wait() Result
	Cancel() error
}
type Provider interface {
	Diagnose(context.Context) Diagnostics
	Start(context.Context, RunRequest) (RunHandle, error)
	Resume(context.Context, ResumeRequest) (RunHandle, error)
	Cancel(context.Context, string) error
}
