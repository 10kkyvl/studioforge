package api

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/10kkyvl/studioforge/internal/config"
	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/diagnostics"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/gitcheckpoint"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/platform/toolpath"
	"github.com/10kkyvl/studioforge/internal/projects"
	"github.com/10kkyvl/studioforge/internal/prompts"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/scheduler"
	"github.com/10kkyvl/studioforge/internal/webui"
)

//go:embed openapi.yaml
var apiFiles embed.FS

// StudioOpener builds a project's place file and opens it in Roblox Studio.
// *studio.Opener satisfies it; tests substitute a fake so no Studio is spawned.
type StudioOpener interface {
	OpenProject(ctx context.Context, projectPath, name, id string) (placePath string, err error)
}

type Server struct {
	store        *database.Store
	db           *database.DB
	scheduler    *scheduler.Manager
	hub          *events.Hub
	doctor       *diagnostics.Doctor
	sessions     *SessionManager
	guard        *projects.PathGuard
	safeMode     bool
	allowedHost  string
	dataDir      string
	logger       *slog.Logger
	applySetting func(string, string) error
	assets       fs.FS
	csp          string
	detectMu     sync.Mutex
	detected     map[string][]toolpath.Candidate
	detectedAt   time.Time
	studio       StudioOpener
	studioStatus func(context.Context, string) (StudioStatus, error)
	syncer       Syncer
}
type Dependencies struct {
	Store                *database.Store
	DB                   *database.DB
	Scheduler            *scheduler.Manager
	Hub                  *events.Hub
	Doctor               *diagnostics.Doctor
	Sessions             *SessionManager
	Guard                *projects.PathGuard
	SafeMode             bool
	AllowedHost, DataDir string
	Logger               *slog.Logger
	ApplySetting         func(string, string) error
	Studio               StudioOpener
	StudioStatus         func(context.Context, string) (StudioStatus, error)
	Sync                 Syncer
}

func New(d Dependencies) (*Server, error) {
	assets, err := fs.Sub(webui.Files, "dist")
	if err != nil {
		return nil, err
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &Server{store: d.Store, db: d.DB, scheduler: d.Scheduler, hub: d.Hub, doctor: d.Doctor, sessions: d.Sessions, guard: d.Guard, safeMode: d.SafeMode, allowedHost: d.AllowedHost, dataDir: d.DataDir, logger: d.Logger, applySetting: d.ApplySetting, studio: d.Studio, studioStatus: d.StudioStatus, syncer: d.Sync, assets: assets, csp: contentSecurityPolicy(assets)}, nil
}

func contentSecurityPolicy(assets fs.FS) string {
	_ = assets
	return "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("POST /api/v1/session/bootstrap", s.bootstrap)
	mux.HandleFunc("GET /api/v1/snapshot", s.snapshot)
	mux.HandleFunc("GET /api/v1/events", s.sse)
	mux.HandleFunc("GET /api/v1/diagnostics", s.doctorHandler)
	mux.HandleFunc("POST /api/v1/settings", s.settings)
	mux.HandleFunc("GET /api/v1/detect-paths", s.detectPaths)
	mux.HandleFunc("GET /api/v1/studio-status", s.studioStatusHandler)
	mux.HandleFunc("POST /api/v1/projects", s.createProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/archive", s.archiveProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/open-studio", s.openStudio)
	mux.HandleFunc("POST /api/v1/projects/{id}/sync", s.startSync)
	mux.HandleFunc("DELETE /api/v1/projects/{id}/sync", s.stopSync)
	mux.HandleFunc("POST /api/v1/projects/{id}/tasks", s.createTaskHandler)
	mux.HandleFunc("POST /api/v1/tasks/{taskId}", s.updateTaskHandler)
	mux.HandleFunc("DELETE /api/v1/tasks/{taskId}", s.deleteTaskHandler)
	mux.HandleFunc("POST /api/v1/projects/{id}/agents", s.createAgent)
	mux.HandleFunc("POST /api/v1/projects/{id}/agents/{agentID}", s.updateAgent)
	mux.HandleFunc("GET /api/v1/projects/{id}/threads", s.listThreads)
	mux.HandleFunc("POST /api/v1/projects/{id}/threads", s.createThread)
	mux.HandleFunc("GET /api/v1/projects/{id}/lead", s.getLead)
	mux.HandleFunc("POST /api/v1/projects/{id}/lead", s.setLead)
	mux.HandleFunc("GET /api/v1/projects/{id}/pace", s.pace)
	mux.HandleFunc("POST /api/v1/projects/{id}/attachments", s.uploadAttachment)
	mux.HandleFunc("GET /api/v1/projects/{id}/attachments/{name}", s.getAttachment)
	mux.HandleFunc("GET /api/v1/threads/{threadId}/messages", s.threadMessages)
	mux.HandleFunc("POST /api/v1/runs", s.createRun)
	mux.HandleFunc("POST /api/v1/runs/{id}/{action}", s.runAction)
	mux.HandleFunc("POST /api/v1/studios/{id}/bind", s.bindStudio)
	mux.HandleFunc("POST /api/v1/backups", s.backup)
	mux.HandleFunc("GET /api/v1/openapi.yaml", s.openapi)
	mux.HandleFunc("/", s.static)
	return s.security(mux)
}

func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = database.NewID()
		}
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", s.csp)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if !s.validHost(r.Host) {
				writeError(w, r, http.StatusBadRequest, "invalid_host", "Request Host is not the StudioForge listener", nil)
				return
			}
			if isMutation(r.Method) && !s.validOrigin(r) {
				writeError(w, r, http.StatusForbidden, "invalid_origin", "Mutating requests require a matching Origin", nil)
				return
			}
			public := r.URL.Path == "/api/v1/health" || r.URL.Path == "/api/v1/session/bootstrap"
			if !public {
				cookie, err := r.Cookie("studioforge_session")
				if err != nil || !s.sessions.Valid(cookie.Value) {
					writeError(w, r, http.StatusUnauthorized, "unauthorized", "A valid local StudioForge session is required", nil)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) validHost(hostport string) bool { return strings.EqualFold(hostport, s.allowedHost) }
func (s *Server) validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && strings.EqualFold(parsed.Host, r.Host)
}
func isMutation(method string) bool {
	return method != "GET" && method != "HEAD" && method != "OPTIONS"
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": config.Version})
}
func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	session, err := s.sessions.Exchange(body.Token)
	if err != nil {
		writeError(w, r, 401, "invalid_bootstrap", err.Error(), nil)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "studioforge_session", Value: session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectsList, err := s.store.ListProjects(ctx, true)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to list projects", err)
		return
	}
	for i := range projectsList {
		projectsList[i].Sync = s.syncStatus(projectsList[i].ID)
	}
	runs, err := s.store.ListRuns(ctx, "", 200)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to list runs", err)
		return
	}
	agents, err := s.store.ListAgents(ctx, "")
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to list agents", err)
		return
	}
	tasks, err := s.store.ListTasks(ctx, "")
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to list tasks", err)
		return
	}
	studios, err := s.store.ListStudioSessions(ctx)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to list Studio sessions", err)
		return
	}
	locale, ok, _ := s.store.Setting(ctx, "locale")
	if !ok {
		locale = "auto"
	}
	setup, setupDone, _ := s.store.Setting(ctx, "setup_complete")
	settings := map[string]any{"locale": locale, "setupComplete": setupDone && setup == "true", "safeMode": s.safeMode}
	defaults := map[string]string{
		"default_provider": "codex", "default_model": "default", "default_effort": "medium",
		"codex_path": "", "claude_path": "", "rojo_path": "", "git_path": "", "studio_mcp_path": "", "studio_auto_open": "true", "concurrency": "6",
	}
	for key, fallback := range defaults {
		value, ok, _ := s.store.Setting(ctx, key)
		if !ok {
			value = fallback
		}
		settings[key] = value
	}
	writeJSON(w, 200, map[string]any{"projects": projectsList, "runs": runs, "agents": agents, "tasks": tasks, "studios": studios, "diagnostics": s.doctor.Run(ctx), "settings": settings})
}

// detectPaths reports where the external tools appear to be installed, so the
// Settings form can fill itself in.
//
// It takes no input on purpose: it executes what it finds to read a version, so
// accepting a caller-supplied path would turn it into "run this executable".
func (s *Server) detectPaths(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, r, 400, "unexpected_input", "Path detection takes no parameters", nil)
		return
	}
	if cached, ok := s.cachedDetection(); ok {
		writeJSON(w, 200, map[string]any{"tools": cached})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	found := toolpath.DetectAll(ctx)
	if err := ctx.Err(); err != nil {
		// The operator navigating away cancels the request mid-probe, which marks
		// every candidate as failed. Caching that would report the tools as broken
		// for the next half minute and suppress autofill.
		writeError(w, r, 503, "detection_incomplete", "Path detection did not finish", err)
		return
	}
	s.storeDetection(found)
	writeJSON(w, 200, map[string]any{"tools": found})
}

const detectionTTL = 30 * time.Second

func (s *Server) cachedDetection() (map[string][]toolpath.Candidate, bool) {
	s.detectMu.Lock()
	defer s.detectMu.Unlock()
	if s.detected == nil || time.Since(s.detectedAt) >= detectionTTL {
		return nil, false
	}
	return s.detected, true
}

// storeDetection keeps the lock off the probing itself, which spawns a process
// per candidate; holding it across that would stall every concurrent caller for
// the full timeout.
func (s *Server) storeDetection(found map[string][]toolpath.Candidate) {
	s.detectMu.Lock()
	s.detected, s.detectedAt = found, time.Now()
	s.detectMu.Unlock()
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	allowed := map[string]bool{
		"locale": true, "theme": true, "setup_complete": true, "concurrency": true,
		"default_provider": true, "default_model": true, "default_effort": true,
		"codex_path": true, "claude_path": true, "rojo_path": true, "git_path": true, "studio_mcp_path": true, "studio_auto_open": true,
	}
	for key, value := range body {
		if !allowed[key] {
			writeError(w, r, 400, "invalid_setting", "Unsupported setting: "+key, nil)
			return
		}
		if key == "locale" && value != "en" && value != "ru" && value != "auto" {
			writeError(w, r, 400, "invalid_locale", "Locale must be en, ru, or auto", nil)
			return
		}
		if key == "default_provider" && value != "codex" && value != "claude" && value != "mock" {
			writeError(w, r, 400, "invalid_provider", "Default provider must be codex, claude, or mock", nil)
			return
		}
		if key == "default_effort" && value != "low" && value != "medium" && value != "high" && value != "xhigh" {
			writeError(w, r, 400, "invalid_effort", "Default effort must be low, medium, high, or xhigh", nil)
			return
		}
		if key == "concurrency" {
			count, err := strconv.Atoi(value)
			if err != nil || count < 1 || count > 32 {
				writeError(w, r, 400, "invalid_concurrency", "Concurrency must be between 1 and 32", nil)
				return
			}
		}
	}
	// Validate the complete request before persisting any entry. Map iteration
	// order is intentionally undefined, so validation and mutation must be
	// separate to prevent a rejected request from partially changing settings.
	for key, value := range body {
		if err := s.store.SetSetting(r.Context(), key, value); err != nil {
			writeError(w, r, 500, "database_error", "Unable to save settings", err)
			return
		}
		if s.applySetting != nil {
			if err := s.applySetting(key, value); err != nil {
				writeError(w, r, 400, "setting_apply_failed", err.Error(), nil)
				return
			}
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, Path, Description string
		Create                  bool
		OpenStudio              bool
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Path) == "" {
		writeError(w, r, 400, "validation", "Name and path are required", nil)
		return
	}
	if body.Create {
		if err := os.MkdirAll(body.Path, 0o700); err != nil {
			writeError(w, r, 400, "path_error", "Unable to create project directory", err)
			return
		}
	}
	id := database.NewID()
	root, err := s.guard.Register(id, body.Path)
	if err != nil {
		writeError(w, r, 400, "path_error", err.Error(), nil)
		return
	}
	project, err := s.store.CreateProject(r.Context(), models.Project{ID: id, Name: strings.TrimSpace(body.Name), Path: root, Fingerprint: projects.Fingerprint(root), Description: body.Description})
	if err != nil {
		writeError(w, r, 409, "project_conflict", "Unable to register project", err)
		return
	}
	if err := projects.Scaffold(root, project.Name); err != nil {
		writeError(w, r, 500, "scaffold_failed", "Project was registered but its Rojo skeleton could not be written", err)
		return
	}
	if body.OpenStudio && s.studio != nil {
		// Best effort: a launch failure is a notice, not a create failure, so
		// the project response below still returns 201 regardless.
		_, _ = s.studio.OpenProject(r.Context(), project.Path, project.Name, project.ID)
	}
	provider, ok, _ := s.store.Setting(r.Context(), "default_provider")
	if !ok || provider == "" {
		provider = "codex"
	}
	model, ok, _ := s.store.Setting(r.Context(), "default_model")
	if !ok || model == "" {
		model = "default"
	}
	effort, ok, _ := s.store.Setting(r.Context(), "default_effort")
	if !ok || effort == "" {
		effort = "medium"
	}
	if _, _, err := s.store.EnsureDefaultAgent(r.Context(), project.ID, provider, model, effort); err != nil {
		writeError(w, r, 500, "agent_create_failed", "Project was registered but its default agent could not be created", err)
		return
	}
	// A freshly minted ID can never already hold a sync session, but setting it
	// explicitly keeps every returned project payload going through the same
	// path rather than leaving this one to rely on the zero value by accident.
	project.Sync = s.syncStatus(project.ID)
	writeJSON(w, 201, project)
}

func normalizeAgent(agent *models.Agent) error {
	agent.Name = strings.TrimSpace(agent.Name)
	agent.Role = strings.TrimSpace(agent.Role)
	agent.Provider = strings.ToLower(strings.TrimSpace(agent.Provider))
	agent.ModelAlias = strings.TrimSpace(agent.ModelAlias)
	agent.Effort = strings.ToLower(strings.TrimSpace(agent.Effort))
	agent.Permission = strings.ToLower(strings.TrimSpace(agent.Permission))
	if agent.Name == "" {
		return errors.New("agent name is required")
	}
	if agent.Role == "" {
		agent.Role = "Roblox Engineer"
	}
	if agent.Provider != "codex" && agent.Provider != "claude" && agent.Provider != "mock" {
		return errors.New("provider must be codex, claude, or mock")
	}
	if agent.ModelAlias == "" {
		agent.ModelAlias = "default"
	}
	if agent.Effort == "" {
		agent.Effort = "medium"
	}
	if agent.Effort != "low" && agent.Effort != "medium" && agent.Effort != "high" && agent.Effort != "xhigh" {
		return errors.New("effort must be low, medium, high, or xhigh")
	}
	if agent.Permission == "" || agent.Permission == "safe" {
		agent.Permission = "workspace-write"
	}
	if agent.Permission != "read-only" && agent.Permission != "workspace-write" && agent.Permission != "danger-full-access" {
		return errors.New("permission must be read-only, workspace-write, or danger-full-access")
	}
	if agent.Concurrency < 1 || agent.Concurrency > 16 {
		return errors.New("agent concurrency must be between 1 and 16")
	}
	if agent.Budget < 0 || agent.Budget > 10000 {
		return errors.New("agent budget must be between 0 and 10000")
	}
	return nil
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Project(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	var agent models.Agent
	if err := decodeJSON(r, &agent); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	agent.ID, agent.ProjectID, agent.Enabled = "", r.PathValue("id"), true
	if agent.Concurrency == 0 {
		agent.Concurrency = 1
	}
	if agent.Budget == 0 {
		agent.Budget = 10
	}
	if err := normalizeAgent(&agent); err != nil {
		writeError(w, r, 400, "validation", err.Error(), nil)
		return
	}
	created, err := s.store.CreateAgent(r.Context(), agent)
	if err != nil {
		writeError(w, r, 409, "agent_conflict", "Unable to create agent", err)
		return
	}
	writeJSON(w, 201, created)
}

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	var agent models.Agent
	if err := decodeJSON(r, &agent); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	agent.ID, agent.ProjectID = r.PathValue("agentID"), r.PathValue("id")
	if err := normalizeAgent(&agent); err != nil {
		writeError(w, r, 400, "validation", err.Error(), nil)
		return
	}
	updated, err := s.store.UpdateAgent(r.Context(), agent)
	if err != nil {
		writeError(w, r, 404, "not_found", "Agent not found", err)
		return
	}
	writeJSON(w, 200, updated)
}
func (s *Server) archiveProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Archived bool `json:"archived"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	if err := s.store.SetProjectArchived(r.Context(), r.PathValue("id"), body.Archived); err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// listThreads ensures a project's default thread exists, then lists all of
// the project's threads, newest first. The GET is always non-empty: a fresh
// project earns its default thread on first read.
func (s *Server) listThreads(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if _, err := s.store.EnsureDefaultThread(r.Context(), projectID); err != nil {
		writeError(w, r, 500, "database_error", "Unable to open chat thread", err)
		return
	}
	threads, err := s.store.ListThreads(r.Context(), projectID)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to list chat threads", err)
		return
	}
	writeJSON(w, 200, map[string]any{"threads": threads})
}

func (s *Server) createThread(w http.ResponseWriter, r *http.Request) {
	var body struct{ Title string }
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	thread, err := s.store.CreateThread(r.Context(), r.PathValue("id"), body.Title)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to create chat thread", err)
		return
	}
	writeJSON(w, 201, thread)
}

// leadAgentSettingKey is the project_settings key under which the chosen
// lead agent's ID is stored.
const leadAgentSettingKey = "lead_agent_id"

func (s *Server) getLead(w http.ResponseWriter, r *http.Request) {
	agentID, _, err := s.store.ProjectSetting(r.Context(), r.PathValue("id"), leadAgentSettingKey)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to read lead agent", err)
		return
	}
	writeJSON(w, 200, map[string]string{"agentId": agentID})
}

func (s *Server) setLead(w http.ResponseWriter, r *http.Request) {
	var body struct{ AgentID string }
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	projectID := r.PathValue("id")
	agents, err := s.store.ListAgents(r.Context(), projectID)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to list project agents", err)
		return
	}
	found := false
	for _, candidate := range agents {
		if candidate.ID == body.AgentID && candidate.Enabled {
			found = true
			break
		}
	}
	if !found {
		writeError(w, r, 400, "agent_missing", "Agent is not an enabled agent of this project", nil)
		return
	}
	if err := s.store.SetProjectSetting(r.Context(), projectID, leadAgentSettingKey, body.AgentID); err != nil {
		writeError(w, r, 500, "database_error", "Unable to save lead agent", err)
		return
	}
	writeJSON(w, 200, map[string]string{"agentId": body.AgentID})
}

func (s *Server) pace(w http.ResponseWriter, r *http.Request) {
	typicalSeconds, samples, err := s.store.TypicalRunSeconds(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to compute run pace", err)
		return
	}
	writeJSON(w, 200, map[string]any{"typicalSeconds": typicalSeconds, "samples": samples})
}

func (s *Server) threadMessages(w http.ResponseWriter, r *http.Request) {
	messages, err := s.store.ThreadMessages(r.Context(), r.PathValue("threadId"))
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to load thread messages", err)
		return
	}
	writeJSON(w, 200, map[string]any{"messages": messages})
}

// StudioStatus is how many Studio instances are open and how many hold the
// place of the project being asked about.
type StudioStatus struct {
	Open    int
	Matched int
	// Blocked means Studio is running but another MCP client holds its
	// connection. It is reported apart from Open because the two look identical
	// from the launcher — both list no instances — while needing opposite advice.
	Blocked bool
}

// studioStatusHandler answers what the chat badge shows. It reports the state
// relative to a project, because "a Studio is open" is not the same question as
// "this project's Studio is open" — every project used to build to the same
// place file name, so the two were indistinguishable.
func (s *Server) studioStatusHandler(w http.ResponseWriter, r *http.Request) {
	if s.studioStatus == nil {
		writeJSON(w, 200, map[string]any{"open": 0, "state": "none"})
		return
	}
	status, err := s.studioStatus(r.Context(), r.URL.Query().Get("project"))
	if err != nil {
		writeJSON(w, 200, map[string]any{"open": 0, "state": "none", "error": err.Error()})
		return
	}
	state := "none"
	switch {
	case status.Matched > 0:
		state = "matched"
	case status.Open > 0:
		state = "other"
	case status.Blocked:
		// Studio is open; it is the connection that is unavailable. Reporting
		// this as "none" sent operators to reopen a Studio already in front of
		// them, and left the real cause — another MCP client owning it — unsaid.
		state = "blocked"
	}
	writeJSON(w, 200, map[string]any{"open": status.Open, "matched": status.Matched, "blocked": status.Blocked, "state": state})
}

func (s *Server) openStudio(w http.ResponseWriter, r *http.Request) {
	if s.studio == nil {
		writeError(w, r, 501, "not_supported", "Opening Studio is not available on this platform", nil)
		return
	}
	project, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	place, err := s.studio.OpenProject(r.Context(), project.Path, project.Name, project.ID)
	if err != nil {
		writeError(w, r, 409, "studio_launch", err.Error(), err)
		return
	}
	writeJSON(w, 200, map[string]any{"place": place})
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	if s.safeMode {
		writeError(w, r, 409, "safe_mode", "AI workers are disabled in safe mode", nil)
		return
	}
	var body struct {
		ProjectID, AgentID, TaskID, Scenario, Prompt, ThreadID, Mode string
		MaxBudget                                                    float64
		Attachments                                                  []string
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		writeError(w, r, 400, "invalid_prompt", "A chat message is required", nil)
		return
	}
	project, err := s.store.Project(r.Context(), body.ProjectID)
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	if strings.TrimSpace(body.TaskID) != "" {
		task, err := s.store.Task(r.Context(), body.TaskID)
		if err != nil {
			writeError(w, r, 400, "task_missing", "Task not found", err)
			return
		}
		if task.ProjectID != project.ID {
			writeError(w, r, 400, "task_not_in_project", "Task does not belong to project", nil)
			return
		}
		if err := s.store.SetTaskStatus(r.Context(), task.ID, "running"); err != nil {
			writeError(w, r, 500, "database_error", "Unable to update task status", err)
			return
		}
		body.Prompt = buildTaskPrompt(task, body.Prompt)
	}
	if len(body.Attachments) > 0 {
		for _, path := range body.Attachments {
			if !validAttachmentRef(project.Path, path) {
				writeError(w, r, 400, "invalid_attachment", "Attachment path is invalid: "+path, nil)
				return
			}
		}
		body.Prompt = appendAttachmentsBlock(body.Prompt, body.Attachments)
	}
	agents, err := s.store.ListAgents(r.Context(), body.ProjectID)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to list project agents", err)
		return
	}
	enabled := make([]models.Agent, 0, len(agents))
	for _, candidate := range agents {
		if candidate.Enabled {
			enabled = append(enabled, candidate)
		}
	}
	if len(enabled) == 0 {
		writeError(w, r, 400, "agent_missing", "Project has no enabled agent", err)
		return
	}
	agent := enabled[0]
	switch {
	case body.AgentID != "":
		found := false
		for _, candidate := range enabled {
			if candidate.ID == body.AgentID {
				agent = candidate
				found = true
				break
			}
		}
		if !found {
			writeError(w, r, 400, "agent_missing", "Agent does not belong to project", nil)
			return
		}
	default:
		// No explicit agent was requested: prefer the project's lead agent, if
		// one is set and still enabled, before falling back to enabled[0].
		if leadID, ok, _ := s.store.ProjectSetting(r.Context(), body.ProjectID, leadAgentSettingKey); ok && leadID != "" {
			for _, candidate := range enabled {
				if candidate.ID == leadID {
					agent = candidate
					break
				}
			}
		}
	}
	diag, configured := s.scheduler.Diagnose(r.Context(), agent.Provider)
	if !configured {
		writeError(w, r, 409, "provider_missing", "Provider is not configured: "+agent.Provider, nil)
		return
	}
	if !diag.Available {
		writeError(w, r, 409, "provider_unavailable", diag.Message, nil)
		return
	}
	if agent.Provider != "mock" && !diag.Authenticated {
		writeError(w, r, 409, "provider_auth", diag.Message, nil)
		return
	}
	maxBudget := body.MaxBudget
	if maxBudget <= 0 {
		maxBudget = agent.Budget
	}
	var thread models.ChatThread
	if body.ThreadID != "" {
		if candidate, err := s.store.ThreadByID(r.Context(), body.ThreadID); err == nil && candidate.ProjectID == project.ID {
			thread = candidate
		}
	}
	if thread.ID == "" {
		thread, err = s.store.EnsureDefaultThread(r.Context(), project.ID)
		if err != nil {
			writeError(w, r, 500, "database_error", "Unable to open chat thread", err)
			return
		}
	}
	resumeSession, err := s.store.LatestThreadSession(r.Context(), thread.ID)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to read chat history", err)
		return
	}
	projectContext := projects.LoadContext(project.Path)
	var subagents []providers.Subagent
	if strings.Contains(strings.ToLower(agent.Role), "orchestrator") {
		for _, candidate := range enabled {
			if candidate.ID == agent.ID {
				continue
			}
			// Delegated work is forwarded to the operator too, so a subagent needs the
			// same rules about language and scope as the orchestrator that spawned it.
			subagents = append(subagents, providers.Subagent{Name: candidate.Name, Description: candidate.Role, Prompt: prompts.ForRun(candidate.SystemPrompt, "")})
		}
	}
	// Carry the house rules and the project's standing context so the operator need
	// not re-explain the project — or which language to answer in — on every message.
	systemPrompt := prompts.ForRun(agent.SystemPrompt, projectContext)
	if agent.Provider == "claude" && body.Mode != "plan" {
		// Snapshot the project so the operator can revert an agent's edits. Best
		// effort: a non-git project or a hook failure never blocks the run, but the
		// failure is logged so an operator who loses `git revert` coverage can find out why.
		if _, checkpointErr := gitcheckpoint.Checkpoint(project.Path, "StudioForge checkpoint before agent run"); checkpointErr != nil {
			s.logger.Warn("git checkpoint failed", "project_id", project.ID, "project_path", project.Path, "error", checkpointErr)
		}
	}
	key := r.Header.Get("Idempotency-Key")
	run, created, err := s.scheduler.Submit(r.Context(), scheduler.Job{ProjectID: project.ID, AgentID: agent.ID, TaskID: body.TaskID, Provider: agent.Provider, Model: agent.ModelAlias, Effort: agent.Effort, PermissionProfile: agent.Permission, WorkingDirectory: project.Path, Prompt: body.Prompt, SystemPrompt: systemPrompt, Mode: body.Mode, ThreadID: thread.ID, ResumeSessionID: resumeSession, Scenario: body.Scenario, MaxBudget: maxBudget, Resources: []string{"project:" + project.ID + ":write"}, IdempotencyKey: key, Subagents: subagents})
	if err != nil {
		writeError(w, r, 400, "run_error", err.Error(), nil)
		return
	}
	status := 201
	if !created {
		status = 200
	}
	writeJSON(w, status, run)
}

func (s *Server) runAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	// Safe mode disables AI workers. Pause and cancel stay available because they only
	// stop existing work; resume and restart would put a worker back on the queue.
	if s.safeMode && (action == "resume" || action == "restart") {
		writeError(w, r, 409, "safe_mode", "AI workers are disabled in safe mode", nil)
		return
	}
	var err error
	switch action {
	case "pause":
		err = s.scheduler.Pause(r.Context(), id)
	case "resume":
		err = s.scheduler.Resume(r.Context(), id)
	case "cancel":
		err = s.scheduler.Cancel(r.Context(), id)
	case "restart":
		run, runErr := s.store.Run(r.Context(), id)
		if runErr != nil {
			err = runErr
		} else if run.Status != "interrupted" && run.Status != "failed" && run.Status != "cancelled" {
			err = fmt.Errorf("run in status %s cannot be restarted", run.Status)
		} else {
			project, pErr := s.store.Project(r.Context(), run.ProjectID)
			if pErr != nil {
				err = pErr
			} else {
				agents, agentErr := s.store.ListAgents(r.Context(), run.ProjectID)
				if agentErr != nil {
					err = agentErr
				} else {
					var agent *models.Agent
					for index := range agents {
						if agents[index].ID == run.AgentID && agents[index].Enabled {
							agent = &agents[index]
							break
						}
					}
					if agent == nil {
						err = errors.New("the original agent is missing or disabled")
					} else {
						_, _, err = s.scheduler.Submit(r.Context(), scheduler.Job{ProjectID: run.ProjectID, AgentID: run.AgentID, TaskID: run.TaskID, Provider: agent.Provider, Model: agent.ModelAlias, Effort: agent.Effort, PermissionProfile: agent.Permission, WorkingDirectory: project.Path, Prompt: "Restart the interrupted task. Inspect the previous failure and complete the task with verification.", SystemPrompt: prompts.ForRun(agent.SystemPrompt, projects.LoadContext(project.Path)), MaxBudget: agent.Budget, Resources: []string{"project:" + run.ProjectID + ":write"}})
					}
				}
			}
		}
	default:
		writeError(w, r, 404, "not_found", "Unknown run action", nil)
		return
	}
	if err != nil {
		writeError(w, r, 409, "run_action_failed", err.Error(), nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) bindStudio(w http.ResponseWriter, r *http.Request) {
	var body struct{ ProjectID string }
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	if err := s.store.BindStudio(r.Context(), r.PathValue("id"), body.ProjectID); err != nil {
		writeError(w, r, 409, "studio_binding", "Unable to bind Studio session", err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	target := filepath.Join(s.dataDir, "backups", "studioforge-"+time.Now().UTC().Format("20060102-150405")+".db")
	if err := s.db.Backup(r.Context(), target); err != nil {
		writeError(w, r, 500, "backup_failed", "Unable to create database backup", err)
		return
	}
	writeJSON(w, 201, map[string]string{"path": target})
}
func (s *Server) doctorHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.doctor.Run(r.Context()))
}
func (s *Server) openapi(w http.ResponseWriter, r *http.Request) {
	body, _ := apiFiles.ReadFile("openapi.yaml")
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(body)
}

func (s *Server) sse(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, 500, "stream_unsupported", "Streaming is unavailable", nil)
		return
	}
	// The browser's native EventSource reconnect sends Last-Event-ID and must win
	// over a stale query-string "after", or reconnects would replay already-seen
	// events. The query parameter only applies when there is no header, e.g. an
	// explicit initial deep-link load that isn't a native EventSource reconnect.
	var after int64
	if header := r.Header.Get("Last-Event-ID"); header != "" {
		after, _ = strconv.ParseInt(header, 10, 64)
	} else if value := r.URL.Query().Get("after"); value != "" {
		after, _ = strconv.ParseInt(value, 10, 64)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	replay, err := s.store.EventsAfter(r.Context(), after, r.URL.Query().Get("projectId"), r.URL.Query().Get("runId"), 1000)
	if err != nil {
		writeError(w, r, 500, "replay_failed", "Unable to replay events", err)
		return
	}
	send := func(event models.RunEvent) error {
		body, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, body)
		if err == nil {
			flusher.Flush()
		}
		return err
	}
	for _, event := range replay {
		if err := send(event); err != nil {
			return
		}
	}
	stream, cancel := s.hub.Subscribe(256)
	defer cancel()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-stream:
			if !ok {
				return
			}
			if event.ID <= after {
				continue
			}
			if err := send(event); err != nil {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(r.URL.Path)), "/")
	if path == "" {
		path = "index.html"
	}
	if strings.Contains(path, "..") {
		http.NotFound(w, r)
		return
	}
	body, err := fs.ReadFile(s.assets, path)
	if err != nil {
		// Only client-side routes (paths with no file extension, e.g. /settings or
		// /projects/abc) fall back to index.html so the SvelteKit router can take over.
		// A missing asset that DOES have an extension (.js, .css, .woff2, ...) is a real
		// 404: silently serving index.html for it used to make the browser try to execute
		// HTML as a JS module, and the app never booted.
		if filepath.Ext(path) != "" {
			http.NotFound(w, r)
			return
		}
		path = "index.html"
		body, err = fs.ReadFile(s.assets, path)
	}
	if err != nil {
		http.Error(w, "embedded frontend unavailable", 500)
		return
	}
	// path is reassigned to "index.html" above on fallback, so the content type below is
	// always derived from what was actually written, never from the originally requested path.
	w.Header().Set("Content-Type", contentTypeFor(path))
	_, _ = w.Write(body)
}

// contentTypeFor hardcodes the MIME types for the file kinds the frontend actually ships
// instead of trusting mime.TypeByExtension: on Windows that function reads type associations
// out of the registry, and machines with a stripped-down or mangled registry can report bogus
// types for .js/.css (observed: text/plain). Responses carry X-Content-Type-Options: nosniff
// (see the security middleware), so a wrong type here doesn't just mislead the browser, it
// hard-fails module loading. Anything outside this ship list still falls back to the registry.
func contentTypeFor(path string) string {
	switch filepath.Ext(path) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	case ".html":
		return "text/html; charset=utf-8"
	}
	if contentType := mime.TypeByExtension(filepath.Ext(path)); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, internal error) {
	requestID := w.Header().Get("X-Request-ID")
	if internal != nil {
		slog.Default().Error("request failed", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "error", internal)
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "requestId": requestID}})
}

var _ = context.Canceled
var _ = errors.Is
var _ = sql.ErrNoRows
var _ = net.IPv4len
