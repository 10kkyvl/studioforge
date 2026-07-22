package nvidia

import (
	"net/http"
	"time"

	"github.com/10kkyvl/studioforge/internal/processes"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter"
)

const (
	BaseURL     = "https://integrate.api.nvidia.com/v1"
	FreeTierRPM = 40
)

type Model struct {
	ID              string
	Name            string
	ContextLength   int
	MaxOutputTokens int
	Vision          bool
	Tools           bool
	Description     string
}

var supportedModels = []Model{
	{
		ID:              "z-ai/glm-5.2",
		Name:            "GLM-5.2",
		ContextLength:   1_000_000,
		MaxOutputTokens: 16_384,
		Tools:           true,
		Description:     "Flagship model for long-horizon agentic workflows, coding, and reasoning.",
	},
	{
		ID:              "nvidia/nemotron-3-ultra-550b-a55b",
		Name:            "Nemotron 3 Ultra 550B A55B",
		ContextLength:   1_000_000,
		MaxOutputTokens: 16_384,
		Tools:           true,
		Description:     "NVIDIA hybrid Mamba-Transformer MoE for planning, coding, and tool use.",
	},
	{
		ID:              "moonshotai/kimi-k2.6",
		Name:            "Kimi K2.6",
		ContextLength:   262_144,
		MaxOutputTokens: 16_384,
		Vision:          true,
		Tools:           true,
		Description:     "Multimodal MoE for long-horizon coding, agentic tool use, and image understanding.",
	},
	{
		ID:              "deepseek-ai/deepseek-v4-pro",
		Name:            "DeepSeek V4 Pro",
		ContextLength:   1_000_000,
		MaxOutputTokens: 16_384,
		Tools:           true,
		Description:     "Large MoE model for coding, reasoning, and agentic tool use.",
	},
}

func Models() []Model {
	return append([]Model(nil), supportedModels...)
}

func FindModel(id string) (Model, bool) {
	for _, model := range supportedModels {
		if model.ID == id {
			return model, true
		}
	}
	return Model{}, false
}

func New(sup *processes.Supervisor) *openrouter.Provider {
	return NewWithHTTPClient(sup, NewHTTPClient())
}

func NewHTTPClient() *http.Client {
	return newRateLimitedHTTPClient(FreeTierRPM)
}

func NewWithHTTPClient(sup *processes.Supervisor, client *http.Client) *openrouter.Provider {
	routing := false
	provider := openrouter.NewCompatible(sup, openrouter.CompatibleConfig{
		ID:              "nvidia",
		DisplayName:     "NVIDIA",
		EnvVar:          "NVIDIA_API_KEY",
		BaseURL:         BaseURL,
		HTTPClient:      client,
		ProviderRouting: &routing,
	})
	provider.SetModelInfo(modelInfo)
	return provider
}

func modelInfo(id string) (openrouter.ModelInfo, bool) {
	model, ok := FindModel(id)
	if !ok {
		return openrouter.ModelInfo{}, false
	}
	return openrouter.ModelInfo{
		Vision:            model.Vision,
		Tools:             model.Tools,
		Verified:          true,
		CapabilitiesKnown: true,
		PriceKnown:        true,
		ContextLength:     model.ContextLength,
		MaxOutputTokens:   model.MaxOutputTokens,
	}, true
}

func newRateLimitedHTTPClient(rpm int) *http.Client {
	if rpm <= 0 {
		rpm = FreeTierRPM
	}
	return &http.Client{
		Timeout: 5 * time.Minute,
		Transport: &pacedTransport{
			base:     http.DefaultTransport,
			interval: time.Minute / time.Duration(rpm),
		},
	}
}
