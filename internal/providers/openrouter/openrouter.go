package openrouter

import (
	"context"
	"net/http"
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
	Vision            bool
	Tools             bool
	Verified          bool
	CapabilitiesKnown bool
	PriceKnown        bool
	PromptPrice       float64
	CompletionPrice   float64
	RequestPrice      float64
	ImagePrice        float64
	CacheReadPrice    float64
	CacheWritePrice   float64
	ReasoningPrice    float64
	ContextLength     int
	MaxOutputTokens   int
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
	mu              sync.Mutex
	runs            map[string]context.CancelFunc
	sup             *processes.Supervisor
	id              string
	display         string
	envVar          string
	baseURL         string
	httpClient      *http.Client
	providerRouting bool
	keySource       func() string
	connector       MCPConnector
	store           ConversationStore
	modelInfo       ModelInfoFunc
	routing         RoutingOptions
}

func New(sup *processes.Supervisor) *Provider {
	return NewCompatible(sup, CompatibleConfig{})
}

// CompatibleConfig adapts the built-in coding-agent loop to another
// OpenAI-compatible chat-completions service. Zero values preserve the
// OpenRouter behavior used by New.
type CompatibleConfig struct {
	ID              string
	DisplayName     string
	EnvVar          string
	BaseURL         string
	HTTPClient      *http.Client
	ProviderRouting *bool
}

func NewCompatible(sup *processes.Supervisor, cfg CompatibleConfig) *Provider {
	if cfg.ID == "" {
		cfg.ID = "openrouter"
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = "OpenRouter"
	}
	if cfg.EnvVar == "" {
		cfg.EnvVar = "OPENROUTER_API_KEY"
	}
	providerRouting := true
	if cfg.ProviderRouting != nil {
		providerRouting = *cfg.ProviderRouting
	}
	return &Provider{
		runs:            map[string]context.CancelFunc{},
		sup:             sup,
		id:              cfg.ID,
		display:         cfg.DisplayName,
		envVar:          cfg.EnvVar,
		baseURL:         cfg.BaseURL,
		httpClient:      cfg.HTTPClient,
		providerRouting: providerRouting,
		keySource:       func() string { return os.Getenv(cfg.EnvVar) },
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
	display := p.display
	id := p.id
	envVar := p.envVar
	p.mu.Unlock()
	key := ""
	if keySource != nil {
		key = keySource()
	}
	authed := key != ""
	msg := display + " API key configured"
	if !authed {
		msg = "Set " + envVar + " or add a key in Settings"
	}
	return providers.Diagnostics{
		Available:     true,
		Authenticated: authed,
		Version:       id + "-http",
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
