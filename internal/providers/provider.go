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
	RunID, ProjectID, AgentID, WorkingDirectory, Prompt, SystemPrompt, Mode, Model, Effort, PermissionProfile string
	MaxTurns                                                                                                  int
	MaxBudget                                                                                                 float64
	MCPConfigPath                                                                                             string
	// AllowedTools names the tools the run may call without an interactive
	// approval, which non-interactive runs cannot answer.
	AllowedTools []string
	Environment  []string
	Scenario     string
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
type Event struct {
	Type      string    `json:"type"`
	RawType   string    `json:"rawType"`
	Payload   any       `json:"payload"`
	SessionID string    `json:"sessionId,omitempty"`
	Cost      float64   `json:"cost,omitempty"`
	Error     string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}
type Result struct {
	SessionID string
	Cost      float64
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
