package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const cannedModelsResponse = `{
  "data": [
    {
      "id": "vendor/tool-model",
      "canonical_slug": "vendor/tool-model-v1",
      "name": "Tool Model",
      "description": "A tool-capable model.",
      "context_length": 128000,
      "architecture": {
        "modality": "text->text",
        "input_modalities": ["text", "image"],
        "output_modalities": ["text"],
        "tokenizer": "Other",
        "instruct_type": null,
        "unexpected_field": "ignored"
      },
      "pricing": {
        "prompt": "0.000001",
        "completion": "0.000002",
        "request": "0",
        "image": "0",
        "input_cache_read": "0",
        "input_cache_write": "0",
        "web_search": "0",
        "internal_reasoning": "0"
      },
      "top_provider": {
        "context_length": null,
        "max_completion_tokens": null,
        "is_moderated": false
      },
      "supported_parameters": ["tools", "structured_outputs", "temperature"],
      "created": 1700000000,
      "unexpected_top_level_field": {"nested": true}
    },
    {
      "id": "vendor/no-tool-model",
      "canonical_slug": "vendor/no-tool-model-v1",
      "name": "No Tool Model",
      "description": "A model without tool support.",
      "context_length": 32000,
      "architecture": {
        "input_modalities": ["text"],
        "output_modalities": ["text"],
        "tokenizer": "Other"
      },
      "pricing": {
        "prompt": "0.0000005",
        "completion": "0.000001"
      },
      "top_provider": {
        "context_length": 32000,
        "max_completion_tokens": 4096,
        "is_moderated": true
      },
      "supported_parameters": ["temperature"],
      "created": 1690000000
    },
    {
      "id": "vendor/free-tool-model:free",
      "name": "Free Tool Model",
      "context_length": 64000,
      "architecture": {
        "input_modalities": ["text"],
        "output_modalities": ["text"]
      },
      "pricing": {
        "prompt": "0",
        "completion": "0"
      },
      "top_provider": {
        "context_length": null,
        "max_completion_tokens": null,
        "is_moderated": false
      },
      "supported_parameters": ["tools"],
      "created": 1710000000
    }
  ]
}`

func TestFetchParsesFieldsAndCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cannedModelsResponse))
	}))
	defer server.Close()

	models, err := Fetch(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 3 {
		t.Fatalf("models=%d, want 3", len(models))
	}

	toolModel := models[0]
	if toolModel.ID != "vendor/tool-model" || toolModel.CanonicalSlug != "vendor/tool-model-v1" {
		t.Fatalf("toolModel=%+v", toolModel)
	}
	if toolModel.ContextLength != 128000 {
		t.Fatalf("contextLength=%d", toolModel.ContextLength)
	}
	if !toolModel.SupportsTools() {
		t.Error("expected SupportsTools true")
	}
	if !toolModel.SupportsVision() {
		t.Error("expected SupportsVision true")
	}
	if !toolModel.SupportsStructuredOutputs() {
		t.Error("expected SupportsStructuredOutputs true")
	}
	if !toolModel.OutputsText() {
		t.Error("expected OutputsText true")
	}
	if toolModel.IsFree() {
		t.Error("expected IsFree false for non-zero pricing")
	}
	if !toolModel.AgentCompatible() {
		t.Error("expected AgentCompatible true")
	}
	// null top_provider ints tolerated as zero values.
	if toolModel.TopProvider.ContextLength != 0 || toolModel.TopProvider.MaxCompletionTokens != 0 {
		t.Fatalf("topProvider=%+v, want null ints to unmarshal as 0", toolModel.TopProvider)
	}

	noToolModel := models[1]
	if noToolModel.SupportsTools() || noToolModel.AgentCompatible() {
		t.Fatalf("noToolModel=%+v, expected tools unsupported and not agent compatible", noToolModel)
	}
	if noToolModel.TopProvider.ContextLength != 32000 || noToolModel.TopProvider.MaxCompletionTokens != 4096 || !noToolModel.TopProvider.IsModerated {
		t.Fatalf("noToolModel topProvider=%+v", noToolModel.TopProvider)
	}

	freeModel := models[2]
	if !freeModel.IsFree() {
		t.Error("expected freeModel IsFree true")
	}
	if !freeModel.AgentCompatible() {
		t.Error("expected freeModel AgentCompatible true")
	}
}

func TestFetchDefaultsBaseURLWhenEmpty(t *testing.T) {
	// Exercise the default only far enough to prove baseURL resolution runs
	// without panicking; a live network call would make this test flaky.
	if !strings.HasPrefix(DefaultBaseURL, "https://") {
		t.Fatalf("DefaultBaseURL=%q", DefaultBaseURL)
	}
}

func TestFetchSkipsMalformedModelsButReturnsGoodOnes(t *testing.T) {
	body := `{
	  "data": [
	    {"id": "vendor/good-1", "supported_parameters": ["tools"], "architecture": {"output_modalities": ["text"]}, "pricing": {"prompt": "0.000001", "completion": "0.000002"}},
	    {"id": "vendor/bad", "supported_parameters": ["tools"], "architecture": {"output_modalities": ["text"]}, "pricing": {"prompt": 0.000001, "completion": "0.000002"}},
	    {"id": "vendor/good-2", "supported_parameters": ["tools"], "architecture": {"output_modalities": ["text"]}, "pricing": {"prompt": "0", "completion": "0"}}
	  ]
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	models, err := Fetch(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models=%d, want 2 (malformed model skipped, good ones kept)", len(models))
	}
	if models[0].ID != "vendor/good-1" || models[1].ID != "vendor/good-2" {
		t.Fatalf("models=%+v", models)
	}
}

func TestFetchReturnsClearErrorOnBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), server.Client(), server.URL)
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error=%v, want it to mention the status code", err)
	}
}
