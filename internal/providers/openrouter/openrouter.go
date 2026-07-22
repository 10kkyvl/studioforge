package openrouter

import (
	"context"
	"os"
	"sync"

	"github.com/10kkyvl/studioforge/internal/processes"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
)

type MCPGrant struct {
	Client       *mcp.Client
	AllowedTools []string
	Context      string
	Notice       string
	Release      func()
}

type MCPConnector func(ctx context.Context, projectID, runID, permissionProfile string) MCPGrant

// ModelInfo is the catalog-derived facts about a model the provider needs to
// gate vision requests and estimate cost when OpenRouter reports none.
type ModelInfo struct {
	Vision          bool
	PromptPrice     float64
	CompletionPrice float64
	ContextLength   int
}

// ModelInfoFunc looks up a model by ID, reporting ok=false when the catalog
// has no record of it (an unknown model is treated as not vision-capable).
type ModelInfoFunc func(modelID string) (ModelInfo, bool)

// RoutingOptions are the operator-configurable OpenRouter provider-routing
// preferences. RequireParameters is deliberately not configurable here: the
// provider always forces it on as a non-negotiable safe default.
type RoutingOptions struct {
	AllowFallbacks *bool
	DataCollection string
	ZDR            bool
	Order          []string
}

type Provider struct {
	mu        sync.Mutex
	runs      map[string]context.CancelFunc
	sup       *processes.Supervisor
	baseURL   string
	keySource func() string
	connector MCPConnector
	store     ConversationStore
	modelInfo ModelInfoFunc
	routing   RoutingOptions
}

func New(sup *processes.Supervisor) *Provider {
	return &Provider{
		runs:      map[string]context.CancelFunc{},
		sup:       sup,
		keySource: func() string { return os.Getenv("OPENROUTER_API_KEY") },
	}
}

func (p *Provider) SetBaseURL(url string) {
	p.mu.Lock()
	p.baseURL = url
	p.mu.Unlock()
}

func (p *Provider) SetKeySource(fn func() string) {
	p.mu.Lock()
	p.keySource = fn
	p.mu.Unlock()
}

func (p *Provider) SetMCPConnector(fn MCPConnector) {
	p.mu.Lock()
	p.connector = fn
	p.mu.Unlock()
}

func (p *Provider) SetConversationStore(cs ConversationStore) {
	p.mu.Lock()
	p.store = cs
	p.mu.Unlock()
}

func (p *Provider) SetModelInfo(fn ModelInfoFunc) {
	p.mu.Lock()
	p.modelInfo = fn
	p.mu.Unlock()
}

func (p *Provider) SetRouting(r RoutingOptions) {
	p.mu.Lock()
	p.routing = r
	p.mu.Unlock()
}

func (p *Provider) Diagnose(context.Context) providers.Diagnostics {
	p.mu.Lock()
	keySource := p.keySource
	p.mu.Unlock()
	key := ""
	if keySource != nil {
		key = keySource()
	}
	authed := key != ""
	msg := "OpenRouter API key configured"
	if !authed {
		msg = "Set OPENROUTER_API_KEY or add a key in Settings"
	}
	return providers.Diagnostics{
		Available:     true,
		Authenticated: authed,
		Version:       "openrouter-http",
		Path:          "built-in",
		Capabilities:  map[string]bool{"tools": true, "streaming": true, "resume": true},
		Message:       msg,
	}
}

func (p *Provider) Cancel(_ context.Context, runID string) error {
	p.mu.Lock()
	cancel, ok := p.runs[runID]
	p.mu.Unlock()
	if !ok {
		return nil
	}
	cancel()
	return nil
}
