package catalog

import "strings"

type Architecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Tokenizer        string   `json:"tokenizer"`
	InstructType     string   `json:"instruct_type"`
}

type Pricing struct {
	Prompt            string `json:"prompt"`
	Completion        string `json:"completion"`
	Request           string `json:"request"`
	Image             string `json:"image"`
	InputCacheRead    string `json:"input_cache_read"`
	InputCacheWrite   string `json:"input_cache_write"`
	WebSearch         string `json:"web_search"`
	InternalReasoning string `json:"internal_reasoning"`
}

type TopProvider struct {
	ContextLength       int  `json:"context_length"`
	MaxCompletionTokens int  `json:"max_completion_tokens"`
	IsModerated         bool `json:"is_moderated"`
}

type Model struct {
	ID                  string       `json:"id"`
	CanonicalSlug       string       `json:"canonical_slug"`
	Name                string       `json:"name"`
	Description         string       `json:"description"`
	ContextLength       int          `json:"context_length"`
	Architecture        Architecture `json:"architecture"`
	Pricing             Pricing      `json:"pricing"`
	TopProvider         TopProvider  `json:"top_provider"`
	SupportedParameters []string     `json:"supported_parameters"`
	Created             int64        `json:"created"`
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func (m Model) SupportsTools() bool {
	return containsString(m.SupportedParameters, "tools")
}

func (m Model) SupportsVision() bool {
	return containsString(m.Architecture.InputModalities, "image")
}

func (m Model) SupportsStructuredOutputs() bool {
	return containsString(m.SupportedParameters, "structured_outputs")
}

func (m Model) OutputsText() bool {
	if len(m.Architecture.OutputModalities) == 0 {
		return true
	}
	return containsString(m.Architecture.OutputModalities, "text")
}

func (m Model) IsFree() bool {
	if m.Pricing.Prompt == "0" && m.Pricing.Completion == "0" {
		return true
	}
	return strings.HasSuffix(m.ID, ":free")
}

func (m Model) AgentCompatible() bool {
	return m.OutputsText() && m.SupportsTools()
}
