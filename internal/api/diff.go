package api

import (
	"database/sql"
	"errors"
	"net/http"
)

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
	if s.git == nil {
		writeJSON(w, 200, map[string]string{"diff": "", "note": "Diffing is not available"})
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
		writeJSON(w, 200, map[string]string{"diff": "", "note": "Unable to compute diff: " + err.Error()})
		return
	}
	response := map[string]any{"diff": diff}
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
