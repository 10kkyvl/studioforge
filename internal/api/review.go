package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/gitops"
	"github.com/10kkyvl/studioforge/internal/gitops/diffparse"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

type reviewHunkInput struct {
	Path  string `json:"path"`
	Index int    `json:"index"`
}

type reviewSelection struct {
	Files []string               `json:"files"`
	Hunks []gitops.HunkSelection `json:"hunks,omitempty"`
}

func (s *Server) reviewRun(w http.ResponseWriter, r *http.Request) {
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
	review, ok, err := s.store.Review(r.Context(), run.ID)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to look up review", err)
		return
	}
	if !ok {
		writeError(w, r, 404, "not_found", "No review recorded for this run", nil)
		return
	}
	if review.Status == "expired" {
		writeError(w, r, 409, "review_expired", "Review has already expired", nil)
		return
	}
	if review.Status != "pending" {
		writeError(w, r, 409, "review_not_pending", "Review has already been resolved", nil)
		return
	}
	now := time.Now().UTC()
	if !review.ExpiresAt.IsZero() && !now.Before(review.ExpiresAt) {
		if _, err := s.store.ExpireReviews(r.Context(), now); err != nil {
			s.logger.Warn("failed to expire review on resolve attempt", "review_id", review.ID, "run_id", run.ID, "error", err)
		}
		writeError(w, r, 409, "review_expired", "Review has expired", nil)
		return
	}
	if s.leases == nil {
		writeError(w, r, 409, "lease_check_unavailable", "Rollback safety check unavailable", nil)
		return
	}
	if owner := s.leases.Snapshot()["project:"+project.ID+":write"]; owner != "" && owner != "review:"+run.ID {
		writeError(w, r, 409, "project_busy", "A run currently holds this project's write lease; wait for it to finish before resolving the review", nil)
		return
	}
	if s.git == nil {
		writeError(w, r, 409, "git_unavailable", "Git operations are not available", nil)
		return
	}
	var body struct {
		Action string            `json:"action"`
		Files  []string          `json:"files"`
		Hunks  []reviewHunkInput `json:"hunks"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	switch body.Action {
	case "apply":
		s.resolveReviewApply(w, r, run, review)
	case "reject":
		s.resolveReviewReject(w, r, run, project, review)
	case "apply-selected":
		s.resolveReviewApplySelected(w, r, run, project, review, body.Files, body.Hunks)
	default:
		writeError(w, r, 400, "invalid_review_action", "action must be apply, reject, or apply-selected", nil)
	}
}

func (s *Server) resolveReviewApply(w http.ResponseWriter, r *http.Request, run models.Run, review models.RunReview) {
	if err := s.store.ResolveReview(r.Context(), review.ID, "applied", "", time.Now().UTC()); err != nil {
		s.writeReviewResolveError(w, r, err)
		return
	}
	s.finalizeReviewResolution(r.Context(), run, "applied")
	writeJSON(w, 200, map[string]any{"status": "applied", "safetyCommit": "", "revertedFiles": 0, "revertedHunks": 0})
}

func (s *Server) resolveReviewReject(w http.ResponseWriter, r *http.Request, run models.Run, project models.Project, review models.RunReview) {
	rawDiff, err := s.git.DiffCommit(r.Context(), project.Path, review.CheckpointHash)
	if err != nil {
		writeRollbackError(w, r, err)
		return
	}
	parsed := diffparse.Parse(rawDiff)
	files := make([]string, 0, len(parsed.Files))
	for _, f := range parsed.Files {
		files = append(files, f.Path)
	}
	result, err := s.git.SelectiveRollback(r.Context(), project.Path, review.CheckpointHash, "", files, nil)
	if err != nil {
		writeRollbackError(w, r, err)
		return
	}
	s.persistReviewSafetyCheckpoint(r.Context(), run, project, result.SafetyCommit)
	selection, _ := json.Marshal(reviewSelection{Files: files})
	if err := s.store.ResolveReview(r.Context(), review.ID, "rejected", string(selection), time.Now().UTC()); err != nil {
		s.writeReviewResolveError(w, r, err)
		return
	}
	s.finalizeReviewResolution(r.Context(), run, "rejected")
	writeJSON(w, 200, map[string]any{"status": "rejected", "safetyCommit": result.SafetyCommit, "revertedFiles": result.RevertedFiles, "revertedHunks": result.RevertedHunks})
}

func (s *Server) resolveReviewApplySelected(w http.ResponseWriter, r *http.Request, run models.Run, project models.Project, review models.RunReview, keepFiles []string, keepHunksInput []reviewHunkInput) {
	rawDiff, err := s.git.DiffCommit(r.Context(), project.Path, review.CheckpointHash)
	if err != nil {
		writeRollbackError(w, r, err)
		return
	}
	parsed := diffparse.Parse(rawDiff)
	keepHunks := make([]gitops.HunkSelection, 0, len(keepHunksInput))
	for _, h := range keepHunksInput {
		keepHunks = append(keepHunks, gitops.HunkSelection{Path: h.Path, Index: h.Index})
	}
	revertFiles, revertHunks, err := invertSelection(parsed, keepFiles, keepHunks)
	if err != nil {
		writeRollbackError(w, r, err)
		return
	}
	result, err := s.git.SelectiveRollback(r.Context(), project.Path, review.CheckpointHash, "", revertFiles, revertHunks)
	if err != nil {
		writeRollbackError(w, r, err)
		return
	}
	s.persistReviewSafetyCheckpoint(r.Context(), run, project, result.SafetyCommit)
	selection, _ := json.Marshal(reviewSelection{Files: keepFiles, Hunks: keepHunks})
	if err := s.store.ResolveReview(r.Context(), review.ID, "partial", string(selection), time.Now().UTC()); err != nil {
		s.writeReviewResolveError(w, r, err)
		return
	}
	s.finalizeReviewResolution(r.Context(), run, "partial")
	writeJSON(w, 200, map[string]any{"status": "partial", "safetyCommit": result.SafetyCommit, "revertedFiles": result.RevertedFiles, "revertedHunks": result.RevertedHunks})
}

func (s *Server) writeReviewResolveError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, database.ErrReviewAlreadyResolved) {
		writeError(w, r, 409, "review_not_pending", "Review has already been resolved", nil)
		return
	}
	writeError(w, r, 500, "database_error", "Unable to resolve review", err)
}

func (s *Server) persistReviewSafetyCheckpoint(ctx context.Context, run models.Run, project models.Project, safetyCommit string) {
	if safetyCommit == "" {
		return
	}
	safety := models.Checkpoint{ProjectID: project.ID, CommitHash: safetyCommit, Label: "before review resolution of run " + run.ID}
	if err := s.store.CreateCheckpoint(ctx, safety); err != nil {
		s.logger.Warn("persist review safety checkpoint failed", "run_id", run.ID, "project_id", project.ID, "error", err)
	}
}

func (s *Server) finalizeReviewResolution(ctx context.Context, run models.Run, reviewStatus string) {
	_, _ = s.hub.Publish(ctx, models.RunEvent{ProjectID: run.ProjectID, RunID: run.ID, AgentID: run.AgentID, Type: "review", RawType: "api.review", Payload: map[string]any{"status": reviewStatus}, CreatedAt: time.Now().UTC()})
	if scheduler.ValidTransition(run.Status, "completed") {
		if ok, err := s.store.UpdateRunIfStatus(ctx, run.ID, []string{run.Status}, "completed", "completed", "", ""); err != nil {
			s.logger.Warn("failed to complete run after review resolution", "run_id", run.ID, "error", err)
		} else if ok {
			_, _ = s.hub.Publish(ctx, models.RunEvent{ProjectID: run.ProjectID, RunID: run.ID, AgentID: run.AgentID, Type: "status", RawType: "api.review", Payload: map[string]any{"status": "completed", "phase": "completed"}, CreatedAt: time.Now().UTC()})
		}
	}
	s.scheduler.ReleaseReviewLease(run.ID, run.ProjectID)
}

func invertSelection(parsed diffparse.Diff, keepFiles []string, keepHunks []gitops.HunkSelection) ([]string, []gitops.HunkSelection, error) {
	filesByPath := make(map[string]diffparse.DiffFile, len(parsed.Files))
	for _, f := range parsed.Files {
		filesByPath[f.Path] = f
	}
	keepFileSet := make(map[string]bool, len(keepFiles))
	for _, path := range keepFiles {
		if _, ok := filesByPath[path]; !ok {
			return nil, nil, fmt.Errorf("%w: %s", gitops.ErrSelectionUnknown, path)
		}
		keepFileSet[path] = true
	}
	keepHunksByPath := make(map[string]map[int]bool)
	for _, h := range keepHunks {
		f, ok := filesByPath[h.Path]
		if !ok {
			return nil, nil, fmt.Errorf("%w: %s", gitops.ErrSelectionUnknown, h.Path)
		}
		if keepFileSet[h.Path] {
			return nil, nil, fmt.Errorf("%w: %s is selected both as a full file and by hunk", gitops.ErrSelectionUnknown, h.Path)
		}
		if h.Index < 0 || h.Index >= len(f.Hunks) {
			return nil, nil, fmt.Errorf("%w: hunk index %d out of range for %s", gitops.ErrSelectionUnknown, h.Index, h.Path)
		}
		if keepHunksByPath[h.Path] == nil {
			keepHunksByPath[h.Path] = map[int]bool{}
		}
		keepHunksByPath[h.Path][h.Index] = true
	}
	var revertFiles []string
	var revertHunks []gitops.HunkSelection
	for _, f := range parsed.Files {
		if keepFileSet[f.Path] {
			continue
		}
		if kept, ok := keepHunksByPath[f.Path]; ok {
			for idx := range f.Hunks {
				if !kept[idx] {
					revertHunks = append(revertHunks, gitops.HunkSelection{Path: f.Path, Index: idx})
				}
			}
			continue
		}
		revertFiles = append(revertFiles, f.Path)
	}
	return revertFiles, revertHunks, nil
}
