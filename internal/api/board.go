package api

import (
	"net/http"
	"strings"

	"github.com/10kkyvl/studioforge/internal/models"
)

// validTaskStatuses mirrors the tasks table's CHECK(status IN (...)) enum.
var validTaskStatuses = map[string]bool{
	"backlog": true, "ready": true, "blocked": true, "running": true,
	"review": true, "completed": true, "failed": true, "cancelled": true,
}

func (s *Server) createTaskHandler(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if _, err := s.store.Project(r.Context(), projectID); err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	var body struct {
		Title, Description, AcceptanceCriteria string
		Priority                               int
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		writeError(w, r, 400, "validation", "Title is required", nil)
		return
	}
	created, err := s.store.CreateTask(r.Context(), models.Task{
		ProjectID: projectID, Title: title, Description: body.Description,
		AcceptanceCriteria: body.AcceptanceCriteria, Priority: body.Priority,
	})
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to create task", err)
		return
	}
	writeJSON(w, 201, created)
}

func (s *Server) updateTaskHandler(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	existing, err := s.store.Task(r.Context(), taskID)
	if err != nil {
		writeError(w, r, 404, "not_found", "Task not found", err)
		return
	}
	var body struct {
		Title              *string
		Description        *string
		AcceptanceCriteria *string
		Status             *string
		Priority           *int
		BlockedReason      *string
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, 400, "invalid_json", err.Error(), nil)
		return
	}
	if body.Status != nil {
		if !validTaskStatuses[*body.Status] {
			writeError(w, r, 400, "invalid_status", "Unknown task status: "+*body.Status, nil)
			return
		}
		existing.Status = *body.Status
	}
	if body.Title != nil {
		title := strings.TrimSpace(*body.Title)
		if title == "" {
			writeError(w, r, 400, "validation", "Title cannot be blank", nil)
			return
		}
		existing.Title = title
	}
	if body.Description != nil {
		existing.Description = *body.Description
	}
	if body.AcceptanceCriteria != nil {
		existing.AcceptanceCriteria = *body.AcceptanceCriteria
	}
	if body.Priority != nil {
		existing.Priority = *body.Priority
	}
	if body.BlockedReason != nil {
		existing.BlockedReason = *body.BlockedReason
	}
	updated, err := s.store.UpdateTask(r.Context(), existing)
	if err != nil {
		writeError(w, r, 404, "not_found", "Task not found", err)
		return
	}
	writeJSON(w, 200, updated)
}

func (s *Server) deleteTaskHandler(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTask(r.Context(), r.PathValue("taskId")); err != nil {
		writeError(w, r, 404, "not_found", "Task not found", err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// buildTaskPrompt prepends an attached task's context to the operator's
// prompt: "Task: " + title + "\n" + description + "\nAcceptance criteria: "
// + acceptanceCriteria + "\n\n" + userPrompt, skipping empty parts.
func buildTaskPrompt(task models.Task, userPrompt string) string {
	lines := []string{"Task: " + task.Title}
	if strings.TrimSpace(task.Description) != "" {
		lines = append(lines, task.Description)
	}
	if strings.TrimSpace(task.AcceptanceCriteria) != "" {
		lines = append(lines, "Acceptance criteria: "+task.AcceptanceCriteria)
	}
	return strings.Join(lines, "\n") + "\n\n" + userPrompt
}
