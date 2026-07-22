package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/10kkyvl/studioforge/internal/gitops"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/orclient"
)

type ToolSet struct {
	profile Profile
	opts    Options
	tools   map[string]Tool
	order   []string
	cmdSeq  atomic.Int64
}

func NewToolSet(profile Profile, opts Options) (*ToolSet, error) {
	if opts.Workspace == nil {
		return nil, fmt.Errorf("agenttools: workspace is required")
	}
	switch profile {
	case ProfileReadOnly, ProfileWorkspace, ProfileDanger:
	default:
		return nil, fmt.Errorf("agenttools: unknown profile %q", profile)
	}
	if opts.Git == nil {
		opts.Git = gitops.New()
	}
	if opts.MaxReadBytes <= 0 {
		opts.MaxReadBytes = 256 * 1024
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = 64 * 1024
	}
	if opts.CommandTimeout <= 0 {
		opts.CommandTimeout = 120 * time.Second
	}
	needsSupervisor := profile == ProfileWorkspace || profile == ProfileDanger
	if needsSupervisor && opts.Supervisor == nil {
		return nil, fmt.Errorf("agenttools: supervisor is required for profile %q", profile)
	}

	s := &ToolSet{profile: profile, opts: opts, tools: map[string]Tool{}}
	s.register(readOnlyTools(opts)...)
	if needsSupervisor {
		s.register(writeTools(opts)...)
		s.register(s.runCommandTool())
	}
	return s, nil
}

func (s *ToolSet) register(tools ...Tool) {
	for _, t := range tools {
		s.tools[t.Name()] = t
		s.order = append(s.order, t.Name())
	}
}

func (s *ToolSet) Definitions() []orclient.Tool {
	defs := make([]orclient.Tool, 0, len(s.order))
	for _, name := range s.order {
		t := s.tools[name]
		defs = append(defs, orclient.Tool{
			Type: "function",
			Function: orclient.ToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return defs
}

func (s *ToolSet) Names() []string {
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

func (s *ToolSet) Has(name string) bool {
	_, ok := s.tools[name]
	return ok
}

func (s *ToolSet) Execute(ctx context.Context, name string, args json.RawMessage) Result {
	t, ok := s.tools[name]
	if !ok {
		return Result{IsError: true, Content: "unknown tool: " + name}
	}
	return t.Execute(ctx, args)
}
