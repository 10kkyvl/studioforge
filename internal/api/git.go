package api

import (
	"database/sql"
	"errors"
	"net/http"
)

func (s *Server) gitStatus(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	if s.git == nil {
		writeJSON(w, 200, map[string]string{"status": "", "note": "Git status is not available"})
		return
	}
	status, err := s.git.Status(r.Context(), project.Path)
	if err != nil {
		writeJSON(w, 200, map[string]string{"status": "", "note": "Unable to read git status: " + err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": status})
}

func (s *Server) rollbackRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.Run(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Run not found", err)
		return
	}
	project, err := s.store.Project(r.Context(), run.ProjectID)
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	checkpoint, err := s.store.CheckpointForRun(r.Context(), run.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, r, 400, "no_checkpoint", "No checkpoint recorded for this run", nil)
			return
		}
		writeError(w, r, 500, "database_error", "Unable to look up checkpoint", err)
		return
	}
	if s.leases == nil {
		writeError(w, r, 409, "lease_check_unavailable", "Rollback safety check unavailable", nil)
		return
	}
	if owner := s.leases.Snapshot()["project:"+project.ID+":write"]; owner != "" {
		writeError(w, r, 409, "project_busy", "A run currently holds this project's write lease; wait for it to finish before rolling back", nil)
		return
	}
	if s.git == nil {
		writeError(w, r, 409, "git_unavailable", "Git operations are not available", nil)
		return
	}
	branch, err := s.git.SafeRollback(r.Context(), project.Path, checkpoint.CommitHash)
	if err != nil {
		writeError(w, r, 409, "rollback_failed", "Unable to roll back: "+err.Error(), err)
		return
	}
	writeJSON(w, 200, map[string]string{"branch": branch, "commitHash": checkpoint.CommitHash})
}

func (s *Server) gitTag(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	if body.Name == "" {
		writeError(w, r, 400, "invalid_name", "Tag name is required", nil)
		return
	}
	if s.git == nil {
		writeError(w, r, 409, "git_unavailable", "Git operations are not available", nil)
		return
	}
	if err := s.git.Tag(r.Context(), project.Path, body.Name); err != nil {
		writeError(w, r, 409, "tag_failed", "Unable to create tag: "+err.Error(), err)
		return
	}
	writeJSON(w, 200, map[string]string{"name": body.Name})
}
