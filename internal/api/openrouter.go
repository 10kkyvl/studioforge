package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/10kkyvl/studioforge/internal/providers/openrouter/catalog"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/credential"
)

func (s *Server) openrouterStatus(w http.ResponseWriter, r *http.Request) {
	if s.orCreds == nil {
		writeError(w, r, 503, "openrouter_unavailable", "OpenRouter is not configured on this daemon", nil)
		return
	}
	writeJSON(w, 200, s.orCreds.Status(r.Context()))
}

func (s *Server) openrouterSaveKey(w http.ResponseWriter, r *http.Request) {
	if s.orCreds == nil {
		writeError(w, r, 503, "openrouter_unavailable", "OpenRouter is not configured on this daemon", nil)
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	if strings.TrimSpace(body.Key) == "" {
		writeError(w, r, 400, "invalid_key", "An API key is required", nil)
		return
	}
	status, err := s.orCreds.Save(r.Context(), body.Key)
	if err != nil {
		writeError(w, r, 500, "openrouter_save_failed", "Unable to save the OpenRouter API key", err)
		return
	}
	writeJSON(w, 200, status)
}

func (s *Server) openrouterDeleteKey(w http.ResponseWriter, r *http.Request) {
	if s.orCreds == nil {
		writeError(w, r, 503, "openrouter_unavailable", "OpenRouter is not configured on this daemon", nil)
		return
	}
	if err := s.orCreds.Remove(r.Context()); err != nil {
		writeError(w, r, 500, "openrouter_remove_failed", "Unable to remove the OpenRouter API key", err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) openrouterTestKey(w http.ResponseWriter, r *http.Request) {
	if s.orCreds == nil {
		writeError(w, r, 503, "openrouter_unavailable", "OpenRouter is not configured on this daemon", nil)
		return
	}
	status, _ := s.orCreds.TestConnection(r.Context())
	writeJSON(w, 200, map[string]any{
		"state":  status.State,
		"source": status.Source,
		"secure": status.Secure,
		"ok":     status.State == credential.StateConfigured,
	})
}

func (s *Server) openrouterModels(w http.ResponseWriter, r *http.Request) {
	if s.orCatalog == nil {
		writeError(w, r, 503, "openrouter_unavailable", "OpenRouter is not configured on this daemon", nil)
		return
	}
	var (
		all    []catalog.Model
		source catalog.Source
		err    error
	)
	if r.URL.Query().Get("refresh") == "1" {
		all, err = s.orCatalog.Refresh(r.Context())
		source = catalog.SourceLive
	} else {
		all, source, err = s.orCatalog.Models(r.Context())
	}
	if err != nil {
		writeError(w, r, 502, "openrouter_models_failed", "Unable to fetch OpenRouter models", err)
		return
	}
	agentModels := catalog.AgentModels(all)
	available := make(map[string]bool, len(agentModels))
	modelsOut := make([]map[string]any, 0, len(agentModels))
	for _, m := range agentModels {
		available[m.ID] = true
		promptPrice, _ := strconv.ParseFloat(m.Pricing.Prompt, 64)
		completionPrice, _ := strconv.ParseFloat(m.Pricing.Completion, 64)
		modelsOut = append(modelsOut, map[string]any{
			"id":              m.ID,
			"name":            m.Name,
			"contextLength":   m.ContextLength,
			"vision":          m.SupportsVision(),
			"tools":           m.SupportsTools(),
			"structured":      m.SupportsStructuredOutputs(),
			"free":            m.IsFree(),
			"promptPrice":     promptPrice,
			"completionPrice": completionPrice,
		})
	}
	curated := catalog.CuratedModels()
	curatedOut := make([]map[string]any, 0, len(curated))
	for _, c := range curated {
		curatedOut = append(curatedOut, map[string]any{
			"id":             c.ID,
			"category":       c.Category,
			"recommendation": c.Recommendation,
			"workload":       c.Workload,
			"free":           c.Free,
			"vision":         c.SupportsImages,
			"available":      available[c.ID] || c.ID == "openrouter/free",
		})
	}
	writeJSON(w, 200, map[string]any{
		"source":     string(source),
		"models":     modelsOut,
		"curated":    curatedOut,
		"categories": catalog.CategoryOrder,
	})
}

func (s *Server) openrouterCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.orCatalog == nil {
		writeError(w, r, 503, "openrouter_unavailable", "OpenRouter is not configured on this daemon", nil)
		return
	}
	id := r.URL.Query().Get("model")
	if id == "openrouter/free" {
		writeJSON(w, 200, map[string]any{"known": true, "vision": true, "tools": true, "structured": false, "contextLength": 0, "free": true})
		return
	}
	models, _, err := s.orCatalog.Models(r.Context())
	if err != nil {
		writeError(w, r, 502, "openrouter_models_failed", "Unable to fetch OpenRouter models", err)
		return
	}
	m, ok := catalog.FindByID(models, id)
	if !ok {
		writeJSON(w, 200, map[string]any{"known": false, "vision": false, "tools": false, "structured": false, "contextLength": 0, "free": false})
		return
	}
	writeJSON(w, 200, map[string]any{
		"known":         true,
		"vision":        m.SupportsVision(),
		"tools":         m.SupportsTools(),
		"structured":    m.SupportsStructuredOutputs(),
		"contextLength": m.ContextLength,
		"free":          m.IsFree(),
	})
}
