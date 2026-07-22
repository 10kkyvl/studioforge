package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/catalog"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/credential"
)

func validateOpenRouterModel(agent *models.Agent, available []catalog.Model, source catalog.Source) error {
	if agent.Provider != "openrouter" {
		return nil
	}
	model, known := catalog.FindByID(available, agent.ModelAlias)
	verified := known && source == catalog.SourceLive && agent.ModelAlias != "openrouter/free"
	if known && !model.SupportsTools() {
		return errors.New("the selected OpenRouter model does not support tool calling")
	}
	if !verified && !agent.AllowUnverifiedModel {
		return errors.New("the selected OpenRouter model has unverified tool compatibility; confirm the advanced compatibility warning to continue")
	}
	return nil
}

func (s *Server) validateOpenRouterAgent(ctx context.Context, agent *models.Agent, refresh bool) error {
	if agent.Provider != "openrouter" {
		return nil
	}
	if s.orCatalog == nil {
		return errors.New("OpenRouter model catalog is unavailable")
	}
	if refresh {
		available, err := s.orCatalog.Refresh(ctx)
		if err != nil {
			if agent.AllowUnverifiedModel {
				return nil
			}
			return errors.New("OpenRouter model compatibility could not be refreshed; retry or explicitly confirm unverified compatibility")
		}
		return validateOpenRouterModel(agent, available, catalog.SourceLive)
	}
	available, source, err := s.orCatalog.Models(ctx)
	if err != nil {
		return errors.New("OpenRouter model compatibility could not be checked")
	}
	return validateOpenRouterModel(agent, available, source)
}

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
	body.Key = strings.TrimSpace(body.Key)
	if body.Key == "" {
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
	status := s.orCreds.Status(r.Context())
	writeJSON(w, 200, map[string]any{"ok": true, "state": status.State, "source": status.Source, "secure": status.Secure, "active": status.Source != credential.SourceNone})
}

func (s *Server) openrouterTestKey(w http.ResponseWriter, r *http.Request) {
	if s.orCreds == nil {
		writeError(w, r, 503, "openrouter_unavailable", "OpenRouter is not configured on this daemon", nil)
		return
	}
	status, err := s.orCreds.TestConnection(r.Context())
	if err != nil {
		var connectionErr *credential.ConnectionError
		if errors.As(err, &connectionErr) {
			switch connectionErr.Kind {
			case credential.ConnectionMissing:
				writeError(w, r, 409, "openrouter_key_missing", connectionErr.Error(), nil)
			case credential.ConnectionTimeout:
				writeError(w, r, 504, "openrouter_test_timeout", connectionErr.Error(), nil)
			case credential.ConnectionNetwork:
				writeError(w, r, 502, "openrouter_test_network", connectionErr.Error(), nil)
			case credential.ConnectionUpstream:
				writeError(w, r, 502, "openrouter_test_upstream", connectionErr.Error(), nil)
			default:
				writeError(w, r, 500, "openrouter_test_failed", connectionErr.Error(), nil)
			}
			return
		}
		writeError(w, r, 500, "openrouter_test_failed", "Unable to test the OpenRouter API key", nil)
		return
	}
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
	available := make(map[string]bool, len(all))
	modelsOut := make([]map[string]any, 0, len(all))
	for _, m := range all {
		if !m.OutputsText() {
			continue
		}
		if m.SupportsTools() {
			available[m.ID] = true
		}
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
			"verified":        source == catalog.SourceLive && m.ID != "openrouter/free",
		})
	}
	curated := catalog.CuratedModels()
	curatedOut := make([]map[string]any, 0, len(curated))
	for _, c := range curated {
		model, found := catalog.FindByID(all, c.ID)
		verified := found && source == catalog.SourceLive && c.ID != "openrouter/free"
		contextLength := 0
		if verified {
			contextLength = model.ContextLength
		}
		curatedOut = append(curatedOut, map[string]any{
			"id":             c.ID,
			"category":       c.Category,
			"recommendation": c.Recommendation,
			"workload":       c.Workload,
			"free":           c.Free,
			"vision":         verified && model.SupportsVision(),
			"tools":          verified && model.SupportsTools(),
			"contextLength":  contextLength,
			"verified":       verified,
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
		writeJSON(w, 200, map[string]any{"known": false, "vision": false, "tools": false, "structured": false, "contextLength": 0, "free": true, "verified": false})
		return
	}
	models, source, err := s.orCatalog.Models(r.Context())
	if err != nil {
		writeError(w, r, 502, "openrouter_models_failed", "Unable to fetch OpenRouter models", err)
		return
	}
	m, ok := catalog.FindByID(models, id)
	if !ok {
		writeJSON(w, 200, map[string]any{"known": false, "vision": false, "tools": false, "structured": false, "contextLength": 0, "free": false, "verified": false})
		return
	}
	writeJSON(w, 200, map[string]any{
		"known":         true,
		"vision":        m.SupportsVision(),
		"tools":         m.SupportsTools(),
		"structured":    m.SupportsStructuredOutputs(),
		"contextLength": m.ContextLength,
		"free":          m.IsFree(),
		"verified":      source == catalog.SourceLive,
	})
}
