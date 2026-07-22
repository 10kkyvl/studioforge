package catalog

import "testing"

func TestModelCapabilityHelpers(t *testing.T) {
	toolVisionFree := Model{
		ID:                  "vendor/tool-vision-free",
		Architecture:        Architecture{InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}},
		Pricing:             Pricing{Prompt: "0", Completion: "0"},
		SupportedParameters: []string{"tools", "structured_outputs"},
	}
	if !toolVisionFree.SupportsTools() {
		t.Error("expected SupportsTools true")
	}
	if !toolVisionFree.SupportsVision() {
		t.Error("expected SupportsVision true")
	}
	if !toolVisionFree.SupportsStructuredOutputs() {
		t.Error("expected SupportsStructuredOutputs true")
	}
	if !toolVisionFree.OutputsText() {
		t.Error("expected OutputsText true")
	}
	if !toolVisionFree.IsFree() {
		t.Error("expected IsFree true for zero pricing")
	}
	if !toolVisionFree.AgentCompatible() {
		t.Error("expected AgentCompatible true")
	}

	noTools := Model{
		ID:           "vendor/no-tools",
		Architecture: Architecture{InputModalities: []string{"text"}, OutputModalities: []string{"text"}},
		Pricing:      Pricing{Prompt: "0.000001", Completion: "0.000002"},
	}
	if noTools.SupportsTools() {
		t.Error("expected SupportsTools false")
	}
	if noTools.AgentCompatible() {
		t.Error("expected AgentCompatible false when tools unsupported")
	}
	if noTools.IsFree() {
		t.Error("expected IsFree false for non-zero pricing")
	}

	emptyOutputModalities := Model{
		ID:                  "vendor/empty-output",
		SupportedParameters: []string{"tools"},
	}
	if !emptyOutputModalities.OutputsText() {
		t.Error("expected OutputsText true when OutputModalities is empty (tolerant default)")
	}
	if !emptyOutputModalities.AgentCompatible() {
		t.Error("expected AgentCompatible true when OutputModalities empty but tools supported")
	}

	freeSuffix := Model{
		ID:      "vendor/some-model:free",
		Pricing: Pricing{Prompt: "0.000001", Completion: "0.000002"},
	}
	if !freeSuffix.IsFree() {
		t.Error("expected IsFree true for :free suffixed id even with non-zero pricing")
	}
}
