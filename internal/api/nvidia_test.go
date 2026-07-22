package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func TestValidateNVIDIAAgentAcceptsSupportedModels(t *testing.T) {
	for _, id := range []string{
		"z-ai/glm-5.2",
		"nvidia/nemotron-3-ultra-550b-a55b",
		"moonshotai/kimi-k2.6",
		"deepseek-ai/deepseek-v4-pro",
	} {
		agent := &models.Agent{Provider: "nvidia", ModelAlias: id}
		if err := validateNVIDIAAgent(agent); err != nil {
			t.Errorf("validateNVIDIAAgent(%q): %v", id, err)
		}
	}
}

func TestValidateNVIDIAAgentRejectsUnknownModel(t *testing.T) {
	agent := &models.Agent{Provider: "nvidia", ModelAlias: "unknown/model"}
	if err := validateNVIDIAAgent(agent); err == nil {
		t.Fatal("validateNVIDIAAgent accepted an unknown NVIDIA model")
	}
}

func TestNVIDIAModelsReportsFreeTierLimit(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Server{}).nvidiaModels(recorder, httptest.NewRequest("GET", "/api/v1/nvidia/models", nil))
	if recorder.Code != 200 {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body struct {
		RPM    int `json:"rpm"`
		Models []struct {
			ID       string `json:"id"`
			Free     bool   `json:"free"`
			Verified bool   `json:"verified"`
		} `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.RPM != 40 || len(body.Models) != 4 {
		t.Fatalf("response = %+v", body)
	}
	for _, model := range body.Models {
		if !model.Free || !model.Verified {
			t.Errorf("model %q is not marked free and verified", model.ID)
		}
	}
}

func TestNVIDIACapabilitiesMarksKimiAsVision(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/nvidia/capabilities?model=moonshotai%2Fkimi-k2.6", nil)
	(&Server{}).nvidiaCapabilities(recorder, request)
	var body struct {
		Known  bool `json:"known"`
		Vision bool `json:"vision"`
		Tools  bool `json:"tools"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Known || !body.Vision || !body.Tools {
		t.Fatalf("capabilities = %+v", body)
	}
}
