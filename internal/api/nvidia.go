package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers/nvidia"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/credential"
)

func validateNVIDIAAgent(agent *models.Agent) error {
	if agent.Provider != "nvidia" {
		return nil
	}
	model, ok := nvidia.FindModel(agent.ModelAlias)
	if !ok {
		return errors.New("the selected NVIDIA model is not in the supported free-endpoint catalog")
	}
	if !model.Tools {
		return errors.New("the selected NVIDIA model does not support tool calling")
	}
	return nil
}

func (s *Server) nvidiaStatus(w http.ResponseWriter, r *http.Request) {
	if s.nvidiaCreds == nil {
		writeError(w, r, 503, "nvidia_unavailable", "NVIDIA is not configured on this daemon", nil)
		return
	}
	writeJSON(w, 200, s.nvidiaCreds.Status(r.Context()))
}

func (s *Server) nvidiaSaveKey(w http.ResponseWriter, r *http.Request) {
	if s.nvidiaCreds == nil {
		writeError(w, r, 503, "nvidia_unavailable", "NVIDIA is not configured on this daemon", nil)
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	body.Key = strings.TrimSpace(body.Key)
	if body.Key == "" {
		writeError(w, r, 400, "invalid_key", "An API key is required", nil)
		return
	}
	status, err := s.nvidiaCreds.Save(r.Context(), body.Key)
	if err != nil {
		writeError(w, r, 500, "nvidia_save_failed", "Unable to save the NVIDIA API key", err)
		return
	}
	writeJSON(w, 200, status)
}

func (s *Server) nvidiaDeleteKey(w http.ResponseWriter, r *http.Request) {
	if s.nvidiaCreds == nil {
		writeError(w, r, 503, "nvidia_unavailable", "NVIDIA is not configured on this daemon", nil)
		return
	}
	if err := s.nvidiaCreds.Remove(r.Context()); err != nil {
		writeError(w, r, 500, "nvidia_remove_failed", "Unable to remove the NVIDIA API key", err)
		return
	}
	status := s.nvidiaCreds.Status(r.Context())
	writeJSON(w, 200, map[string]any{"ok": true, "state": status.State, "source": status.Source, "secure": status.Secure, "active": status.Source != credential.SourceNone})
}

func (s *Server) nvidiaTestKey(w http.ResponseWriter, r *http.Request) {
	if s.nvidiaCreds == nil {
		writeError(w, r, 503, "nvidia_unavailable", "NVIDIA is not configured on this daemon", nil)
		return
	}
	status, err := s.nvidiaCreds.TestConnection(r.Context())
	if err != nil {
		var connectionErr *credential.ConnectionError
		if errors.As(err, &connectionErr) {
			switch connectionErr.Kind {
			case credential.ConnectionMissing:
				writeError(w, r, 409, "nvidia_key_missing", connectionErr.Error(), nil)
			case credential.ConnectionTimeout:
				writeError(w, r, 504, "nvidia_test_timeout", connectionErr.Error(), nil)
			case credential.ConnectionNetwork:
				writeError(w, r, 502, "nvidia_test_network", connectionErr.Error(), nil)
			case credential.ConnectionUpstream:
				writeError(w, r, 502, "nvidia_test_upstream", connectionErr.Error(), nil)
			default:
				writeError(w, r, 500, "nvidia_test_failed", connectionErr.Error(), nil)
			}
			return
		}
		writeError(w, r, 500, "nvidia_test_failed", "Unable to test the NVIDIA API key", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"state": status.State, "source": status.Source, "secure": status.Secure, "ok": status.State == credential.StateConfigured})
}

func (s *Server) nvidiaModels(w http.ResponseWriter, _ *http.Request) {
	modelsOut := make([]map[string]any, 0, len(nvidia.Models()))
	for _, model := range nvidia.Models() {
		modelsOut = append(modelsOut, map[string]any{
			"id": model.ID, "name": model.Name, "description": model.Description,
			"contextLength": model.ContextLength, "vision": model.Vision,
			"tools": model.Tools, "free": true, "verified": true,
		})
	}
	writeJSON(w, 200, map[string]any{"models": modelsOut, "rpm": nvidia.FreeTierRPM})
}

func (s *Server) nvidiaCapabilities(w http.ResponseWriter, r *http.Request) {
	model, ok := nvidia.FindModel(r.URL.Query().Get("model"))
	if !ok {
		writeJSON(w, 200, map[string]any{"known": false, "vision": false, "tools": false, "contextLength": 0, "free": false, "verified": false})
		return
	}
	writeJSON(w, 200, map[string]any{"known": true, "vision": model.Vision, "tools": model.Tools, "contextLength": model.ContextLength, "free": true, "verified": true})
}
