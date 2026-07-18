package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/10kkyvl/studioforge/internal/models"
)

// Syncer starts and stops a project's Rojo live-sync session (`rojo serve`)
// and reports whether one is running. *rojo.Manager's Start/Stop/Session
// (internal/rojo/manager.go) are the real implementation; internal/app adapts
// them to this shape so internal/api never has to import internal/rojo — the
// same reason the Studio MCP status probe is adapted before it reaches here.
type Syncer interface {
	Start(ctx context.Context, projectID, projectFile string) (models.SyncStatus, error)
	Stop(projectID string) error
	Status(projectID string) models.SyncStatus
}

// syncStatus annotates a project payload with its live sync state. A nil
// syncer (no rojo executable configured) reports "not syncing" rather than
// failing the response the project itself is part of.
func (s *Server) syncStatus(projectID string) models.SyncStatus {
	if s.syncer == nil {
		return models.SyncStatus{}
	}
	return s.syncer.Status(projectID)
}

// startSync starts `rojo serve` on the project's default.project.json — the
// one Rojo project file that registration and the Open Studio build both
// already assume (studio.Opener.OpenProject), so this endpoint takes no path
// of its own to serve.
func (s *Server) startSync(w http.ResponseWriter, r *http.Request) {
	if s.syncer == nil {
		writeError(w, r, 501, "not_supported", "Rojo live-sync is not available", nil)
		return
	}
	project, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	projectFile := filepath.Join(project.Path, "default.project.json")
	if _, err := os.Stat(projectFile); err != nil {
		writeError(w, r, 400, "sync_missing_project_file", "No default.project.json in the project; cannot start Rojo sync", err)
		return
	}
	// context.Background(), not r.Context(): rojo.Manager.Start execs `rojo
	// serve` with this context, and net/http cancels a request's context the
	// moment ServeHTTP returns — which would kill the session within moments of
	// this handler replying 200. The session must outlive the request that
	// started it, same as why studio.LaunchPlace detaches Studio's own process
	// from its caller's context.
	status, err := s.syncer.Start(context.Background(), project.ID, projectFile)
	if err != nil {
		writeError(w, r, 409, "sync_start_failed", err.Error(), err)
		return
	}
	writeJSON(w, 200, status)
}

// stopSync ends the project's Rojo serve session, if one is running. Daemon
// shutdown stops every live session the same way, through
// processes.Supervisor.Close (internal/app.Run) — this handler is only the
// on-demand path to the same Manager.Stop, not a second stop mechanism.
func (s *Server) stopSync(w http.ResponseWriter, r *http.Request) {
	if s.syncer == nil {
		writeError(w, r, 501, "not_supported", "Rojo live-sync is not available", nil)
		return
	}
	projectID := r.PathValue("id")
	// Checked directly rather than inferred from Stop's error: on Windows,
	// gracefully asking a console subprocess with no window to close routinely
	// fails and Terminate reports that failure even though the force-kill that
	// follows it succeeds a moment later (internal/rojo's own tests log this
	// error instead of failing on it, for the same reason). Reporting a 409
	// here on every such hiccup would tell the operator a stop failed when the
	// session is, in fact, on its way out.
	if !s.syncer.Status(projectID).Active {
		writeError(w, r, 409, "sync_not_running", "No live-sync session is running for this project", nil)
		return
	}
	if err := s.syncer.Stop(projectID); err != nil {
		s.logger.Warn("rojo sync stop reported an error; the session may already be terminating", "project", projectID, "error", err)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
