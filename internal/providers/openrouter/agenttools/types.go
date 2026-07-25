package agenttools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/10kkyvl/studioforge/internal/gitops"
	"github.com/10kkyvl/studioforge/internal/processes"
)

type Profile string

const (
	ProfileReadOnly  Profile = "read-only"
	ProfileWorkspace Profile = "workspace-write"
	ProfileDanger    Profile = "danger-full-access"
)

type Result struct {
	Content  string
	IsError  bool
	ImageURL string
}

type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, args json.RawMessage) Result
}

type Options struct {
	Workspace      *Workspace
	Git            *gitops.Client
	Supervisor     *processes.Supervisor
	ProjectID      string
	RunID          string
	MaxReadBytes   int
	MaxOutputBytes int
	CommandTimeout time.Duration
	// Ask delivers a closed question to the operator. Left nil, the question
	// tool is still registered but refuses, telling the agent to decide for
	// itself rather than leaving it waiting for an answer that is not coming.
	Ask AskOperator
}

type funcTool struct {
	name        string
	description string
	schema      json.RawMessage
	exec        func(ctx context.Context, args json.RawMessage) Result
}

func (t *funcTool) Name() string            { return t.name }
func (t *funcTool) Description() string     { return t.description }
func (t *funcTool) Schema() json.RawMessage { return t.schema }
func (t *funcTool) Execute(ctx context.Context, args json.RawMessage) Result {
	return t.exec(ctx, args)
}
