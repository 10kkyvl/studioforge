package api

import (
	"database/sql"
	"errors"
	"net/http"
)

func (s *Server) runStudioChanges(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Run(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, r, 404, "not_found", "Run not found", err)
		return
	}
	changes, err := s.store.StudioChanges(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 500, "internal_error", "Unable to load Studio changes", err)
		return
	}
	writeJSON(w, 200, changes)
}

func (s *Server) runDiff(w http.ResponseWriter, r *http.Request) {
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
	changes, err := s.store.StudioChanges(r.Context(), run.ID)
	if err != nil {
		writeError(w, r, 500, "internal_error", "Unable to load Studio changes", err)
		return
	}
	response := map[string]any{"diff": "", "studioChanges": changes}
	if s.git == nil {
		response["note"] = "Diffing is not available"
		writeJSON(w, 200, response)
		return
	}
	checkpoint, checkpointErr := s.store.CheckpointForRun(r.Context(), run.ID)
	hasCheckpoint := checkpointErr == nil
	if checkpointErr != nil && !errors.Is(checkpointErr, sql.ErrNoRows) {
		s.logger.Warn("checkpoint lookup failed", "run_id", run.ID, "error", checkpointErr)
	}
	var diff string
	if hasCheckpoint {
		diff, err = s.git.DiffCommit(r.Context(), project.Path, checkpoint.CommitHash)
	} else {
		diff, err = s.git.DiffHead(r.Context(), project.Path)
	}
	if err != nil {
		response["note"] = "Unable to compute diff: " + err.Error()
		writeJSON(w, 200, response)
		return
	}
	response["diff"] = diff
	if hasCheckpoint {
		response["checkpoint"] = map[string]any{
			"commitHash": checkpoint.CommitHash,
			"branch":     checkpoint.Branch,
			"label":      checkpoint.Label,
			"createdAt":  checkpoint.CreatedAt,
		}
	}
	writeJSON(w, 200, response)
}
