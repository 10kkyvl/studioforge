package openrouter

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/orclient"
)

func TestCompletionCostAccountsForCacheAndReasoning(t *testing.T) {
	completion := &orclient.Completion{Usage: orclient.Usage{PromptTokens: 100, CompletionTokens: 50}, UsagePresent: true}
	info := ModelInfo{PriceKnown: true, PromptPrice: 0.000002, CompletionPrice: 0.000004, RequestPrice: 0.00001, ImagePrice: 0.00002, CacheReadPrice: 0.0000005, CacheWritePrice: 0.000003, ReasoningPrice: 0.000006}
	messages := []orclient.Message{{Role: "user", Content: []orclient.ContentPart{{Type: "text", Text: "look"}, {Type: "image_url", ImageURL: &orclient.ImageURL{URL: "data:image/png;base64,x"}}}}}
	cost, estimated, known := completionCost(completion, info, true, messages, 70, 10, 20)
	want := 0.00001 + 0.00002 + 20*0.000002 + 70*0.0000005 + 10*0.000003 + 30*0.000004 + 20*0.000006
	if !estimated || !known || cost != want {
		t.Fatalf("cost=%v estimated=%v known=%v want=%v", cost, estimated, known, want)
	}
}

func TestCompletionCostUsesReportedZeroCost(t *testing.T) {
	completion := &orclient.Completion{Usage: orclient.Usage{PromptTokens: 100, CompletionTokens: 50, CostPresent: true}}
	cost, estimated, known := completionCost(completion, ModelInfo{PriceKnown: true, PromptPrice: 1, CompletionPrice: 1}, true, nil, 0, 0, 0)
	if cost != 0 || estimated || !known {
		t.Fatalf("cost=%v estimated=%v known=%v", cost, estimated, known)
	}
}

func TestCompletionCostFailsClosedWhenPricingIsUnknown(t *testing.T) {
	completion := &orclient.Completion{Usage: orclient.Usage{PromptTokens: 100, CompletionTokens: 50}}
	cost, estimated, known := completionCost(completion, ModelInfo{}, false, nil, 0, 0, 0)
	if cost != 0 || estimated || known {
		t.Fatalf("cost=%v estimated=%v known=%v", cost, estimated, known)
	}
}

const testPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func mustDecodeTestPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(testPNGBase64)
	if err != nil {
		t.Fatalf("decode test png: %v", err)
	}
	return data
}

func writeTestPNG(t *testing.T, dir, rel string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir attachments dir: %v", err)
	}
	if err := os.WriteFile(full, mustDecodeTestPNG(t), 0o644); err != nil {
		t.Fatalf("write test png: %v", err)
	}
}

func findImageURLParts(msgs []any) []map[string]any {
	var out []map[string]any
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok || mm["role"] != "user" {
			continue
		}
		parts, ok := mm["content"].([]any)
		if !ok {
			continue
		}
		for _, p := range parts {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if pm["type"] == "image_url" {
				out = append(out, pm)
			}
		}
	}
	return out
}

func TestAgentLoop_CostActualNotEstimated(t *testing.T) {
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{
			Choices: []wireChoice{{Delta: wireDelta{Content: "done"}, FinishReason: "stop"}},
			Usage:   &wireUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, Cost: 0.05},
		}}
	})
	provider := newTestProvider(t, srv)
	dir := t.TempDir()
	req := providers.RunRequest{RunID: "cost-actual", ProjectID: "p1", WorkingDirectory: dir, Prompt: "hi", Model: "test-model"}

	events, result := runProvider(t, provider, req)
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if result.Cost != 0.05 {
		t.Fatalf("result cost = %v, want 0.05", result.Cost)
	}
	usageEvents := findEvents(events, "usage", "openrouter.usage")
	if len(usageEvents) != 1 {
		t.Fatalf("want 1 usage event, got %d", len(usageEvents))
	}
	payload, _ := usageEvents[0].Payload.(map[string]any)
	if payload["estimated"] != false {
		t.Errorf("estimated = %v, want false for an actual cost", payload["estimated"])
	}
}

func TestAgentLoop_CostEstimateFallback(t *testing.T) {
	srv, _ := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{
			Choices: []wireChoice{{Delta: wireDelta{Content: "done"}, FinishReason: "stop"}},
			Usage:   &wireUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, Cost: 0},
		}}
	})
	provider := newTestProvider(t, srv)
	provider.SetModelInfo(func(id string) (ModelInfo, bool) {
		return ModelInfo{Tools: true, Verified: true, PriceKnown: true, PromptPrice: 0.000002, CompletionPrice: 0.000004}, true
	})
	dir := t.TempDir()
	req := providers.RunRequest{RunID: "cost-estimate", ProjectID: "p1", WorkingDirectory: dir, Prompt: "hi", Model: "test-model"}

	events, result := runProvider(t, provider, req)
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	want := 100*0.000002 + 50*0.000004
	if diff := result.Cost - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("result cost = %v, want %v", result.Cost, want)
	}
	usageEvents := findEvents(events, "usage", "openrouter.usage")
	if len(usageEvents) != 1 {
		t.Fatalf("want 1 usage event, got %d", len(usageEvents))
	}
	payload, _ := usageEvents[0].Payload.(map[string]any)
	if payload["estimated"] != true {
		t.Errorf("estimated = %v, want true when cost is estimated", payload["estimated"])
	}
}

func TestAgentLoop_ImageVisionSupportedBuildsDataURL(t *testing.T) {
	dir := t.TempDir()
	rel := ".studioforge/attachments/test.png"
	writeTestPNG(t, dir, rel)

	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "I see the image."}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetModelInfo(func(id string) (ModelInfo, bool) { return ModelInfo{Vision: true, Tools: true, Verified: true}, true })
	req := providers.RunRequest{RunID: "img-vision", ProjectID: "p1", WorkingDirectory: dir, Prompt: "what is this", Model: "vision-model", Attachments: []string{rel}}

	_, result := runProvider(t, provider, req)
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if log.count() != 1 {
		t.Fatalf("want 1 HTTP call, got %d", log.count())
	}
	body := log.body(0)
	msgs, _ := body["messages"].([]any)
	parts := findImageURLParts(msgs)
	if len(parts) != 1 {
		t.Fatalf("want 1 image_url part, got %d: %+v", len(parts), msgs)
	}
	imageURL, _ := parts[0]["image_url"].(map[string]any)
	url, _ := imageURL["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("image_url.url = %q, want a data:image/png;base64, URL", url)
	}
}

func TestAgentLoop_ImageNonVisionModelControlledError(t *testing.T) {
	dir := t.TempDir()
	rel := ".studioforge/attachments/test.png"
	writeTestPNG(t, dir, rel)

	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "should not be reached"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "img-no-vision", ProjectID: "p1", WorkingDirectory: dir, Prompt: "what is this", Model: "text-model", Attachments: []string{rel}}

	events, result := runProvider(t, provider, req)
	if result.Err == nil {
		t.Fatalf("expected a controlled error result, got %+v", result)
	}
	errEvents := findEvents(events, "error", "openrouter.image_unsupported")
	if len(errEvents) != 1 {
		t.Fatalf("want 1 openrouter.image_unsupported error event, got %d: %+v", len(errEvents), events)
	}
	if log.count() != 0 {
		t.Fatalf("expected no HTTP request to be sent when images are unsupported, got %d", log.count())
	}
}

func TestAgentLoop_ImageSkipsNonImageAndEscapingAttachments(t *testing.T) {
	dir := t.TempDir()
	attachDir := filepath.Join(dir, ".studioforge", "attachments")
	if err := os.MkdirAll(attachDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(attachDir, "notes.txt"), []byte("just text, not an image"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "escape.png")
	if err := os.WriteFile(outsideFile, mustDecodeTestPNG(t), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	escapeRel, err := filepath.Rel(dir, outsideFile)
	if err != nil {
		t.Fatalf("compute escaping relative path: %v", err)
	}

	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "ok"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetModelInfo(func(id string) (ModelInfo, bool) { return ModelInfo{Vision: true, Tools: true, Verified: true}, true })
	req := providers.RunRequest{
		RunID: "img-skip", ProjectID: "p1", WorkingDirectory: dir, Prompt: "check", Model: "vision-model",
		Attachments: []string{".studioforge/attachments/notes.txt", escapeRel},
	}

	_, result := runProvider(t, provider, req)
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	body := log.body(0)
	msgs, _ := body["messages"].([]any)
	if parts := findImageURLParts(msgs); len(parts) != 0 {
		t.Errorf("expected no image_url parts (non-image file and escaping path must be skipped), got %+v", parts)
	}
}

func TestAgentLoop_RoutingRequireParametersAlwaysTrue(t *testing.T) {
	dir := t.TempDir()
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
				{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "list_dir", Arguments: `{}`}},
			}}, FinishReason: "tool_calls"}}}}
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "done"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "routing-default", ProjectID: "p1", WorkingDirectory: dir, Prompt: "go", Model: "test-model"}

	_, result := runProvider(t, provider, req)
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if log.count() != 2 {
		t.Fatalf("want 2 HTTP calls, got %d", log.count())
	}
	for i := 0; i < log.count(); i++ {
		body := log.body(i)
		providerBlock, ok := body["provider"].(map[string]any)
		if !ok {
			t.Fatalf("call %d body missing provider block: %+v", i, body)
		}
		if providerBlock["require_parameters"] != true {
			t.Errorf("call %d provider.require_parameters = %v, want true", i, providerBlock["require_parameters"])
		}
	}
}

func TestAgentLoop_RoutingDataCollectionDeny(t *testing.T) {
	dir := t.TempDir()
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "done"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	provider.SetRouting(RoutingOptions{DataCollection: "deny"})
	req := providers.RunRequest{RunID: "routing-deny", ProjectID: "p1", WorkingDirectory: dir, Prompt: "go", Model: "test-model"}

	_, result := runProvider(t, provider, req)
	if result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	body := log.body(0)
	providerBlock, _ := body["provider"].(map[string]any)
	if providerBlock["data_collection"] != "deny" {
		t.Errorf("provider.data_collection = %v, want deny", providerBlock["data_collection"])
	}
	if providerBlock["require_parameters"] != true {
		t.Errorf("provider.require_parameters = %v, want true even with routing overrides set", providerBlock["require_parameters"])
	}
}
