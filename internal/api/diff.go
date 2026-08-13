package api

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/10kkyvl/studioforge/internal/gitops"
	"github.com/10kkyvl/studioforge/internal/gitops/diffparse"
	"github.com/10kkyvl/studioforge/internal/models"
)

// checkpointSummary is the run-diff response's "checkpoint" field: the
// checkpoint a run's diff was computed against, without the row's internal
// id/projectId that callers have no use for.
type checkpointSummary struct {
	CommitHash string    `json:"commitHash"`
	Branch     string    `json:"branch"`
	Label      string    `json:"label"`
	CreatedAt  time.Time `json:"createdAt"`
}

// diffModelResponse builds the #8 structured diff model (stats + files) by
// parsing raw unified-diff text, as a map so runDiff and projectDiff can each
// layer their own extra fields (studioDirectEdits/checkpoint for the former,
// nothing extra for the latter) on top without a shared struct forcing
// fields neither always has.
func diffModelResponse(diff string) map[string]any {
	parsed := diffparse.Parse(diff)
	return map[string]any{"stats": parsed.Stats, "files": parsed.Files}
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
	structured := r.URL.Query().Get("format") == "structured"
	if s.git == nil {
		writeJSON(w, 200, runDiffResponse(structured, "", "Diffing is not available", run.StudioDirectEdits, nil))
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
		writeJSON(w, 200, runDiffResponse(structured, "", "Unable to compute diff: "+err.Error(), run.StudioDirectEdits, nil))
		return
	}
	var checkpointField *checkpointSummary
	if hasCheckpoint {
		checkpointField = &checkpointSummary{CommitHash: checkpoint.CommitHash, Branch: checkpoint.Branch, Label: checkpoint.Label, CreatedAt: checkpoint.CreatedAt}
	}
	writeJSON(w, 200, runDiffResponse(structured, diff, "", run.StudioDirectEdits, checkpointField))
}

// runDiffResponse assembles a run-diff response body. structured selects
// between today's raw {"diff": "..."} shape and the #8 structured
// {"stats", "files"} shape; every other field (note, checkpoint,
// studioDirectEdits) behaves identically either way, including omitting
// note/checkpoint entirely rather than writing them as null when they do not
// apply — the same shape the raw handler always produced, so the default
// (non-structured) response stays byte-for-byte what it was before
// ?format=structured existed.
func runDiffResponse(structured bool, diff, note string, studioDirectEdits bool, checkpoint *checkpointSummary) map[string]any {
	var response map[string]any
	if structured {
		response = diffModelResponse(diff)
	} else {
		response = map[string]any{"diff": diff}
	}
	response["studioDirectEdits"] = studioDirectEdits
	if note != "" {
		response["note"] = note
	}
	if checkpoint != nil {
		response["checkpoint"] = checkpoint
	}
	return response
}

// checkpointListItem is one entry in GET /api/v1/projects/{id}/checkpoints.
type checkpointListItem struct {
	CommitHash string    `json:"commitHash"`
	Branch     string    `json:"branch"`
	Label      string    `json:"label"`
	CreatedAt  time.Time `json:"createdAt"`
	RunID      string    `json:"runId"`
}

func (s *Server) projectCheckpoints(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	checkpoints, err := s.store.CheckpointsForProject(r.Context(), project.ID)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to list checkpoints", err)
		return
	}
	items := make([]checkpointListItem, 0, len(checkpoints))
	for _, c := range checkpoints {
		items = append(items, checkpointListItem{CommitHash: c.CommitHash, Branch: c.Branch, Label: c.Label, CreatedAt: c.CreatedAt, RunID: c.RunID})
	}
	writeJSON(w, 200, map[string]any{"checkpoints": items})
}

// isKnownDiffRef reports whether ref is safe to hand to git for a project
// range diff: either the literal "HEAD" or a commit hash this project
// actually recorded a checkpoint for. Rejecting anything else here, before
// git ever runs, is what keeps an arbitrary or malformed ref from reaching
// the shell and turns it into the named unknown_checkpoint error the API
// promises instead of a raw git failure.
func isKnownDiffRef(ref string, checkpoints []models.Checkpoint) bool {
	if ref == "HEAD" {
		return true
	}
	for _, c := range checkpoints {
		if c.CommitHash == ref {
			return true
		}
	}
	return false
}

func (s *Server) projectDiff(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	from := r.URL.Query().Get("from")
	if from == "" {
		writeError(w, r, 400, "missing_from", "from is required", nil)
		return
	}
	to := r.URL.Query().Get("to")
	if to == "" {
		to = "HEAD"
	}
	checkpoints, err := s.store.CheckpointsForProject(r.Context(), project.ID)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to list checkpoints", err)
		return
	}
	if !isKnownDiffRef(from, checkpoints) {
		writeError(w, r, 404, "unknown_checkpoint", "Unknown checkpoint reference: "+from, nil)
		return
	}
	if !isKnownDiffRef(to, checkpoints) {
		writeError(w, r, 404, "unknown_checkpoint", "Unknown checkpoint reference: "+to, nil)
		return
	}
	if s.git == nil {
		response := diffModelResponse("")
		response["note"] = "Diffing is not available"
		writeJSON(w, 200, response)
		return
	}
	diff, err := s.git.DiffRange(r.Context(), project.Path, from, to)
	if err != nil {
		switch {
		case errors.Is(err, gitops.ErrNotAncestor):
			writeError(w, r, 400, "invalid_range", from+" is not an ancestor of "+to, nil)
		case errors.Is(err, gitops.ErrUnknownRef):
			writeError(w, r, 404, "unknown_checkpoint", "Unknown git reference", nil)
		default:
			response := diffModelResponse("")
			response["note"] = "Unable to compute diff: " + err.Error()
			writeJSON(w, 200, response)
		}
		return
	}
	writeJSON(w, 200, diffModelResponse(diff))
}
