package api

import (
	"database/sql"
	"errors"
	"io"
	"net/http"

	"github.com/10kkyvl/studioforge/internal/gitops"
	"github.com/10kkyvl/studioforge/internal/models"
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
	var body struct {
		Files []string `json:"files"`
		Hunks []struct {
			Path  string `json:"path"`
			Index int    `json:"index"`
		} `json:"hunks"`
	}
	if err := decodeJSON(r, &body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	if len(body.Files) == 0 && len(body.Hunks) == 0 {
		branch, err := s.git.SafeRollback(r.Context(), project.Path, checkpoint.CommitHash)
		if err != nil {
			writeError(w, r, 409, "rollback_failed", "Unable to roll back: "+err.Error(), err)
			return
		}
		writeJSON(w, 200, map[string]string{"branch": branch, "commitHash": checkpoint.CommitHash})
		return
	}
	hunks := make([]gitops.HunkSelection, 0, len(body.Hunks))
	for _, h := range body.Hunks {
		hunks = append(hunks, gitops.HunkSelection{Path: h.Path, Index: h.Index})
	}
	checkpoints, err := s.store.CheckpointsForProject(r.Context(), project.ID)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to look up later checkpoints", err)
		return
	}
	nextCheckpoint := nextCheckpointAfter(checkpoints, checkpoint)
	result, err := s.git.SelectiveRollback(r.Context(), project.Path, checkpoint.CommitHash, nextCheckpoint, body.Files, hunks)
	if err != nil {
		writeRollbackError(w, r, err)
		return
	}
	if result.SafetyCommit != "" {
		safety := models.Checkpoint{ProjectID: project.ID, CommitHash: result.SafetyCommit, Label: "before selective rollback of run " + run.ID}
		if err := s.store.CreateCheckpoint(r.Context(), safety); err != nil {
			s.logger.Warn("persist selective rollback safety checkpoint failed", "run_id", run.ID, "project_id", project.ID, "error", err)
		}
	}
	writeJSON(w, 200, map[string]any{
		"commitHash":    checkpoint.CommitHash,
		"safetyCommit":  result.SafetyCommit,
		"revertedFiles": result.RevertedFiles,
		"revertedHunks": result.RevertedHunks,
	})
}

func writeRollbackError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, gitops.ErrSelectionUnknown):
		writeError(w, r, 400, "invalid_selection", err.Error(), nil)
	case errors.Is(err, gitops.ErrDirtyWorktree):
		writeError(w, r, 409, "dirty_worktree", err.Error(), nil)
	case errors.Is(err, gitops.ErrLaterChanges):
		writeError(w, r, 409, "later_change_conflict", err.Error(), nil)
	case errors.Is(err, gitops.ErrPatchCheckFailed):
		writeError(w, r, 409, "patch_check_failed", err.Error(), nil)
	case errors.Is(err, gitops.ErrNotGitRepo):
		writeError(w, r, 409, "not_git_repo", err.Error(), nil)
	default:
		writeError(w, r, 409, "rollback_failed", "Unable to roll back: "+err.Error(), err)
	}
}

func nextCheckpointAfter(checkpoints []models.Checkpoint, after models.Checkpoint) string {
	var best *models.Checkpoint
	for i := range checkpoints {
		c := checkpoints[i]
		if c.CommitHash == after.CommitHash {
			continue
		}
		if !c.CreatedAt.After(after.CreatedAt) {
			continue
		}
		if best == nil || c.CreatedAt.Before(best.CreatedAt) {
			best = &c
		}
	}
	if best == nil {
		return ""
	}
	return best.CommitHash
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
		if errors.Is(err, gitops.ErrInvalidTagName) {
			writeError(w, r, 400, "invalid_name", "Tag name is invalid: "+err.Error(), nil)
			return
		}
		writeError(w, r, 409, "tag_failed", "Unable to create tag: "+err.Error(), err)
		return
	}
	writeJSON(w, 200, map[string]string{"name": body.Name})
}
